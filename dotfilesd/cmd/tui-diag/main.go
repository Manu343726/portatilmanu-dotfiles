// tui-diag — Standalone tview-based diagnostics TUI.
//
// Connects directly to the dotfilesd daemon via Connect RPC and renders
// an interactive htop-like tree/table browser with live updates.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	dotfilesdv1connect "dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ─── resource state ─────────────────────────────────────────────────────────

type resourceState struct {
	ID, Type, Label, ParentID, Status string
	StartedAt, FinishedAt             int64
	DurationNs                        int64
	Attrs                             map[string]string
}

type localCache struct {
	mu        sync.RWMutex
	resources map[string]*resourceState
	events    []*dotfilesdv1.DiagEvent
}

func newCache() *localCache {
	return &localCache{resources: make(map[string]*resourceState)}
}

func (c *localCache) applyEvent(evt *dotfilesdv1.DiagEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.events = append(c.events, evt)
	if len(c.events) > 1000 {
		c.events = c.events[len(c.events)-1000:]
	}

	res, ok := c.resources[evt.Resource]
	if !ok {
		res = &resourceState{
			ID:    evt.Resource,
			Label: evt.Resource,
			Type:  nodeType(evt.Resource),
			Attrs: make(map[string]string),
		}
		c.resources[evt.Resource] = res
	}
	for k, v := range evt.Attrs {
		res.Attrs[k] = v
	}
	if evt.Parent != "" {
		res.ParentID = evt.Parent
	}
	if evt.Labels["label"] != "" {
		res.Label = evt.Labels["label"]
	}

	switch evt.Labels["event_type"] {
	case "daemon_start", "plugin_spawn", "session_create", "client_connect",
		"executor_open", "bg_task_start", "exec_start", "script_start", "plugin_rpc_open":
		if res.StartedAt == 0 {
			res.StartedAt = evt.TimestampNs
			res.Status = "active"
		}
	case "daemon_stop", "plugin_stop", "session_end", "client_disconnect",
		"executor_close", "bg_task_stop", "exec_stop", "script_stop", "plugin_rpc_close":
		if res.FinishedAt == 0 {
			res.FinishedAt = evt.TimestampNs
			res.DurationNs = evt.TimestampNs - res.StartedAt
			res.Status = "finished"
		}
	case "plugin_crash":
		if res.FinishedAt == 0 {
			res.FinishedAt = evt.TimestampNs
			res.DurationNs = evt.TimestampNs - res.StartedAt
			res.Status = "crashed"
		}
	}
}

func nodeType(id string) string {
	if idx := strings.Index(id, ":"); idx > 0 {
		return id[:idx]
	}
	return id
}

func (c *localCache) filteredResources(f filterSet) []*resourceState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now().UnixNano()
	var out []*resourceState
	for _, r := range c.resources {
		if filterMatch(r, f, now) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt < out[j].StartedAt
	})
	return out
}

type filterSet struct {
	textSearch   string
	typeFilter   string
	statusFilter string
	showIdle     bool
}

func filterMatch(r *resourceState, f filterSet, now int64) bool {
	if f.typeFilter != "" && r.Type != f.typeFilter {
		return false
	}
	if f.statusFilter != "" && r.Status != f.statusFilter {
		return false
	}
	if f.textSearch != "" {
		q := strings.ToLower(f.textSearch)
		if !strings.Contains(strings.ToLower(r.Label), q) &&
			!strings.Contains(strings.ToLower(r.ID), q) &&
			!strings.Contains(strings.ToLower(r.Type), q) {
			return false
		}
	}
	if !f.showIdle && (r.Status == "finished" || r.Status == "crashed") {
		return false
	}
	return true
}

// ─── tview TUI ──────────────────────────────────────────────────────────────

type diagUI struct {
	app            *tview.Application
	statusBar      *tview.TextView
	pages          *tview.Pages
	treeWidget     *tview.TreeView
	tableWidget    *tview.Table
	searchInput    *tview.InputField
	footer         *tview.TextView
	mainFlex       *tview.Flex

	cache          *localCache
	queryClient    dotfilesdv1connect.DiagnosticsQueryServiceClient
	ctx            context.Context

	filters        filterSet
	viewMode       int
	eventCount     int
}

func newDiagUI(ctx context.Context, cache *localCache, qc dotfilesdv1connect.DiagnosticsQueryServiceClient) *diagUI {
	d := &diagUI{
		app:         tview.NewApplication(),
		cache:       cache,
		queryClient: qc,
		ctx:         ctx,
		viewMode:    0,
	}
	d.buildUI()
	return d
}

func (d *diagUI) buildUI() {
	d.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft).
		SetText("[::b] tui-diag [::-]| [::d]connecting...[::-]")

	d.searchInput = tview.NewInputField().
		SetLabel("/ ").
		SetFieldWidth(0).
		SetDoneFunc(func(key tcell.Key) {
			d.filters.textSearch = d.searchInput.GetText()
			if key == tcell.KeyEnter {
				d.refreshViews()
				d.app.SetFocus(d.treeWidget)
			}
			d.searchInput.SetText("")
		})

	d.footer = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft).
		SetText("[gray] Tab:switch  /:search  q:quit  t:type  s:status  i:idle  h:help[]")

	d.treeWidget = tview.NewTreeView().
		SetGraphics(true).
		SetTopLevel(0)

	d.treeWidget.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyTab {
			d.switchToView(1)
		}
	})
	d.treeWidget.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		return d.handleKey(ev)
	})

	d.tableWidget = tview.NewTable().
		SetFixed(2, 0).
		SetSelectable(true, false).
		SetSeparator(tview.Borders.Vertical)

	d.tableWidget.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyTab {
			d.switchToView(0)
		}
	})
	d.tableWidget.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		return d.handleKey(ev)
	})

	d.pages = tview.NewPages().
		AddPage("tree", d.treeWidget, true, true).
		AddPage("table", d.tableWidget, true, false)

	d.mainFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(d.statusBar, 1, 0, false).
		AddItem(d.pages, 0, 1, true).
		AddItem(d.searchInput, 1, 0, false).
		AddItem(d.footer, 1, 0, false)

	d.app.SetRoot(d.mainFlex, true).
		SetFocus(d.treeWidget).
		EnableMouse(false)
}

func (d *diagUI) switchToView(mode int) {
	d.viewMode = mode
	switch mode {
	case 0:
		d.pages.SwitchToPage("tree")
		d.app.SetFocus(d.treeWidget)
	case 1:
		d.pages.SwitchToPage("table")
		d.app.SetFocus(d.tableWidget)
		d.refreshTable()
	}
	d.updateStatusBar()
}

func (d *diagUI) handleKey(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q', 'Q':
			d.app.Stop()
			return nil
		case '/':
			d.app.SetFocus(d.searchInput)
			return nil
		case 't':
			d.cycleFilter(&d.filters.typeFilter, []string{"", "daemon", "plugin", "session", "client", "executor", "bg_task"})
			d.refreshViews()
			return nil
		case 's':
			d.cycleFilter(&d.filters.statusFilter, []string{"", "active", "finished", "crashed"})
			d.refreshViews()
			return nil
		case 'i':
			d.filters.showIdle = !d.filters.showIdle
			d.refreshViews()
			return nil
		case 'h', 'H':
			d.showHelp()
			return nil
		}
	case tcell.KeyTab:
		d.switchToView((d.viewMode + 1) % 2)
		return nil
	case tcell.KeyEsc:
		if d.filters.textSearch != "" {
			d.filters.textSearch = ""
			d.searchInput.SetText("")
			d.refreshViews()
			d.app.SetFocus(d.treeWidget)
			return nil
		}
	}
	return ev
}

func (d *diagUI) cycleFilter(current *string, options []string) {
	for i, o := range options {
		if o == *current {
			*current = options[(i+1)%len(options)]
			return
		}
	}
	*current = options[0]
}

func (d *diagUI) showHelp() {
	text := "[::b]tui-diag help[::-]\n\n" +
		"Tab       switch view (tree/table)\n" +
		"/         search\n" +
		"t         cycle type filter\n" +
		"s         cycle status filter\n" +
		"i         toggle show idle\n" +
		"q         quit\n" +
		"h         this help\n" +
		"\n[gray]Press Esc to close[]"

	modal := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter).SetText(text), 0, 1, false)
	modal.SetBorder(true).SetTitle(" Help ")

	mainRoot := d.mainFlex
	d.app.SetRoot(modal, false)
	d.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter {
			d.app.SetRoot(mainRoot, true)
			d.app.SetInputCapture(nil)
			d.app.SetFocus(d.treeWidget)
			return nil
		}
		return ev
	})
}

func (d *diagUI) refreshViews() {
	d.refreshTree()
	d.refreshTable()
	d.updateStatusBar()
}

func (d *diagUI) refreshTree() {
	all := d.cache.filteredResources(d.filters)

	root := tview.NewTreeNode("dotfilesd runtime").
		SetSelectable(false).
		SetColor(tcell.ColorWhite).
		SetExpanded(true)

	childMap := make(map[string][]*resourceState)
	for _, r := range all {
		if r.ParentID == "" || r.ParentID == r.ID {
			continue
		}
		childMap[r.ParentID] = append(childMap[r.ParentID], r)
	}

	var roots []*resourceState
	for _, r := range all {
		if r.ParentID == "" || !cacheHas(all, r.ParentID) {
			roots = append(roots, r)
		}
	}

	var addNodes func(parent *tview.TreeNode, children []*resourceState)
	addNodes = func(parent *tview.TreeNode, children []*resourceState) {
		for _, r := range children {
			label := nodeType(r.Type) + ":" + r.Label
			statusTag := ""
			switch r.Status {
			case "active", "bg_worker", "running":
				statusTag = " [green]" + r.Status + "[]"
			case "finished", "idle":
				statusTag = " [gray]" + r.Status + "[]"
			case "crashed":
				statusTag = " [red]" + r.Status + "[]"
			case "pending":
				statusTag = " [yellow]" + r.Status + "[]"
			default:
				statusTag = " " + r.Status
			}
			attrs := ""
			if pid := r.Attrs["pid"]; pid != "" {
				attrs += " pid=" + pid
			}
			if dur := r.Attrs["running_for"]; dur != "" {
				attrs += " [gray]up " + dur + "[]"
			} else if dur := r.Attrs["duration"]; dur != "" {
				attrs += " [gray]" + dur + "[]"
			}

			tn := tview.NewTreeNode(label + statusTag + attrs).
				SetReference(r).
				SetExpanded(true).
				SetColor(nodeColor(r))

			if kids := childMap[r.ID]; len(kids) > 0 {
				addNodes(tn, kids)
			}
			parent.AddChild(tn)
		}
	}

	if len(roots) == 0 && len(all) > 0 {
		roots = all
	}
	addNodes(root, roots)

	d.treeWidget.SetRoot(root)
	if len(root.GetChildren()) > 0 {
		d.treeWidget.SetCurrentNode(root.GetChildren()[0])
	}
}

func cacheHas(all []*resourceState, id string) bool {
	for _, r := range all {
		if r.ID == id {
			return true
		}
	}
	return false
}

func nodeColor(r *resourceState) tcell.Color {
	switch r.Type {
	case "daemon":
		return tcell.ColorWhite
	case "plugin":
		return tcell.ColorDodgerBlue
	case "session":
		return tcell.ColorYellow
	case "client":
		return tcell.ColorDarkCyan
	case "executor":
		return tcell.ColorGreen
	case "bg_task":
		return tcell.ColorDarkMagenta
	case "root":
		return tcell.ColorGray
	default:
		return tcell.ColorGray
	}
}

func (d *diagUI) refreshTable() {
	all := d.cache.filteredResources(d.filters)
	d.tableWidget.Clear()

	headers := []string{"TYPE", "LABEL", "STATUS", "STARTED", "DURATION"}
	for ci, h := range headers {
		d.tableWidget.SetCell(0, ci, &tview.TableCell{
			Text:       " " + h + " ",
			Color:      tcell.ColorWhite,
			Attributes: tcell.AttrBold,
			Align:      tview.AlignCenter,
			Expansion:  1,
		})
	}

	for ri, r := range all {
		row := ri + 1
		d.tableWidget.SetCell(row, 0, tview.NewTableCell(r.Type).
			SetTextColor(nodeColor(r)).
			SetExpansion(1))
		d.tableWidget.SetCell(row, 1, tview.NewTableCell(r.Label).
			SetTextColor(tcell.ColorWhite).
			SetExpansion(2))

		sc := tcell.ColorGray
		switch r.Status {
		case "active", "bg_worker", "running":
			sc = tcell.ColorGreen
		case "finished", "idle":
			sc = tcell.ColorGray
		case "crashed":
			sc = tcell.ColorRed
		case "pending":
			sc = tcell.ColorYellow
		}
		d.tableWidget.SetCell(row, 2, tview.NewTableCell(r.Status).
			SetTextColor(sc).SetExpansion(1))

		startedStr := "-"
		if r.StartedAt > 0 {
			startedStr = formatAge(time.Since(time.Unix(0, r.StartedAt)))
		}
		d.tableWidget.SetCell(row, 3, tview.NewTableCell(startedStr).
			SetTextColor(tcell.ColorGray).SetExpansion(1))

		durStr := "-"
		if r.DurationNs > 0 {
			durStr = time.Duration(r.DurationNs).Round(time.Millisecond).String()
		} else if r.Status == "active" && r.StartedAt > 0 {
			durStr = formatAge(time.Since(time.Unix(0, r.StartedAt)))
		}
		d.tableWidget.SetCell(row, 4, tview.NewTableCell(durStr).
			SetTextColor(tcell.ColorGray).SetExpansion(1))
	}
}

func formatAge(d time.Duration) string {
	if d < time.Second {
		return "now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func (d *diagUI) updateStatusBar() {
	parts := ""
	if d.filters.typeFilter != "" {
		parts += "type:" + d.filters.typeFilter + " "
	}
	if d.filters.statusFilter != "" {
		parts += "status:" + d.filters.statusFilter + " "
	}
	if d.filters.textSearch != "" {
		parts += "/" + d.filters.textSearch + " "
	}
	if d.filters.showIdle {
		parts += "[yellow]show_idle[] "
	}
	if parts == "" {
		parts = "[gray]: no filters[]"
	}

	d.statusBar.SetText(fmt.Sprintf("[::b] tui-diag [::-]| [gray]%d events  %d nodes[]  | [green]%s[]  |  %s",
		d.eventCount, len(d.cache.resources),
		map[int]string{0: "Tree", 1: "Table"}[d.viewMode],
		parts))
}

func (d *diagUI) run() error {
	go d.subscribeEvents()
	go func() {
		time.Sleep(300 * time.Millisecond)
		d.app.QueueUpdateDraw(d.refreshViews)
	}()
	return d.app.Run()
}

func (d *diagUI) subscribeEvents() {
	stream, err := d.queryClient.StreamEvents(d.ctx, connect.NewRequest(&dotfilesdv1.StreamEventsRequest{}))
	if err != nil {
		return
	}
	for stream.Receive() {
		evt := stream.Msg()
		d.cache.applyEvent(evt)
		d.eventCount++
		d.app.QueueUpdateDraw(d.refreshViews)
	}
}

func diagNodeTypeToString(t dotfilesdv1.DiagNodeType) string {
	switch t {
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_ROOT:
		return "root"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_DAEMON:
		return "daemon"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_CLIENT:
		return "client"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_EXECUTOR:
		return "executor"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_SESSION:
		return "session"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_PLUGIN:
		return "plugin"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_BG_TASK:
		return "bg_task"
	case dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_SHELL:
		return "shell"
	default:
		return "unknown"
	}
}

func diagNodeStatusToString(s dotfilesdv1.DiagNodeStatus) string {
	switch s {
	case dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_ACTIVE:
		return "active"
	case dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_PENDING:
		return "pending"
	case dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_FINISHED:
		return "finished"
	case dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_CRASHED:
		return "crashed"
	default:
		return "unknown"
	}
}

// ─── daemon URL ─────────────────────────────────────────────────────────────

func daemonURL() string {
	if url := os.Getenv("DOTFILESD_URL"); url != "" {
		return url
	}
	if port := os.Getenv("DOTFILESD_PORT"); port != "" {
		return "http://127.0.0.1:" + port
	}
	return "http://127.0.0.1:9105"
}

// ─── main ───────────────────────────────────────────────────────────────────

func main() {
	url := daemonURL()

	queryClient := dotfilesdv1connect.NewDiagnosticsQueryServiceClient(
		http.DefaultClient,
		url,
	)

	ctx := context.Background()

	treeResp, err := queryClient.QueryTree(ctx, connect.NewRequest(&dotfilesdv1.QueryTreeRequest{}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect to daemon at %s: %v\n", url, err)
		os.Exit(1)
	}

	cache := newCache()
	populateCache(cache, treeResp.Msg.Root)

	diag := newDiagUI(ctx, cache, queryClient)

	if err := diag.run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}

func populateCache(cache *localCache, node *dotfilesdv1.DiagNode) {
	if node == nil {
		return
	}
	var walk func(n *dotfilesdv1.DiagNode, parentID string)
	walk = func(n *dotfilesdv1.DiagNode, parentID string) {
		if n == nil {
			return
		}
		typeStr := diagNodeTypeToString(n.Type)
		id := typeStr + ":" + n.Label
		res := &resourceState{
			ID:       id,
			Type:     typeStr,
			Label:    n.Label,
			ParentID: parentID,
			Status:   diagNodeStatusToString(n.Status),
			Attrs:    make(map[string]string),
		}
		if nsStr := n.Attrs["running_for_ns"]; nsStr != "" {
			if ns, err := parseInt64(nsStr); err == nil {
				res.StartedAt = time.Now().UnixNano() - ns
			}
		}
		if nsStr := n.Attrs["duration_ns"]; nsStr != "" {
			if ns, err := parseInt64(nsStr); err == nil {
				res.DurationNs = ns
			}
		}
		for k, v := range n.Attrs {
			res.Attrs[k] = v
		}
		cache.resources[id] = res
		for _, child := range n.Children {
			walk(child, id)
		}
	}
	walk(node, "")
}

func parseInt64(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %s", s)
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
