package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dotfilesd/plugin"
	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	dotfilesdv1connect "dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
	pb "plugins/tuidiag/proto/tuidiag"
	"plugins/tuidiag/proto/tuidiag/tuidiagconnect"

	"connectrpc.com/connect"
)

// ─── resource state (same model as engine) ─────────────────────────────────

type resourceState struct {
	ID         string
	Type       string
	Label      string
	ParentID   string
	Status     string
	CreatedAt  int64
	StartedAt  int64
	FinishedAt int64
	DurationNs int64
	Attrs      map[string]string
	ExitCode   int32
}

type localCache struct {
	mu        sync.RWMutex
	resources map[string]*resourceState
	events    []*dotfilesdv1.DiagEvent
	version   uint64
}

func newCache() *localCache {
	return &localCache{resources: make(map[string]*resourceState)}
}

func (c *localCache) applyEvent(evt *dotfilesdv1.DiagEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.events = append(c.events, evt)
	if len(c.events) > 500 {
		c.events = c.events[len(c.events)-500:]
	}

	res, ok := c.resources[evt.Resource]
	if !ok {
		res = &resourceState{
			ID:    evt.Resource,
			Label: evt.Resource,
			Attrs: make(map[string]string),
		}
		if t := nodeTypeFromResource(evt.Resource); t != "" {
			res.Type = t
		}
		if evt.TimestampNs > 0 {
			res.CreatedAt = evt.TimestampNs
			res.StartedAt = evt.TimestampNs
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

	switch evt.Type {
	case "daemon_start", "plugin_spawn", "session_create", "client_connect",
		"executor_open", "bg_task_start", "exec_start":
		if res.StartedAt == 0 {
			res.StartedAt = evt.TimestampNs
			res.Status = "active"
		}
	case "daemon_stop", "plugin_stop", "session_end", "client_disconnect",
		"executor_close", "bg_task_stop", "exec_stop":
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
	case "metric_report":
		// metadata only — handled by attrs merge above
	}

	c.version++
}

func nodeTypeFromResource(id string) string {
	if idx := strings.Index(id, ":"); idx > 0 {
		return id[:idx]
	}
	return id
}

// ─── tree reconstruction (from diagnostics spec §4.4) ──────────────────────

type treeNode struct {
	entry    *resourceState
	children []*treeNode
	expanded bool
	depth    int
}

func (c *localCache) buildTree(filters filterSet) []*treeNode {
	c.mu.RLock()
	defer c.mu.RUnlock()

	snapshot := make([]*resourceState, 0, len(c.resources))
	for _, res := range c.resources {
		snapshot = append(snapshot, res)
	}

	keep := make([]*resourceState, 0, len(snapshot))
	now := time.Now().UnixNano()

	for _, res := range snapshot {
		if !filterMatch(res, filters, now) {
			continue
		}
		keep = append(keep, res)
	}

	childrenOf := make(map[string][]*resourceState)
	var roots []*resourceState

	for _, res := range keep {
		if res.ParentID == "" {
			roots = append(roots, res)
		} else {
			childrenOf[res.ParentID] = append(childrenOf[res.ParentID], res)
		}
	}

	sort.Slice(roots, func(i, j int) bool {
		return roots[i].StartedAt < roots[j].StartedAt
	})

	var result []*treeNode
	for _, r := range roots {
		result = append(result, buildNode(r, childrenOf, 0))
	}
	return result
}

func buildNode(res *resourceState, childrenOf map[string][]*resourceState, depth int) *treeNode {
	n := &treeNode{
		entry:    res,
		expanded: true,
		depth:    depth,
	}
	if kids, ok := childrenOf[res.ID]; ok {
		sort.Slice(kids, func(i, j int) bool {
			return kids[i].StartedAt < kids[j].StartedAt
		})
		for _, k := range kids {
			n.children = append(n.children, buildNode(k, childrenOf, depth+1))
		}
	}
	return n
}

func (n *treeNode) countVisible() int {
	if !n.expanded || len(n.children) == 0 {
		return 1
	}
	c := 1
	for _, ch := range n.children {
		c += ch.countVisible()
	}
	return c
}

func (n *treeNode) visibleNodes() []*treeNode {
	var out []*treeNode
	n.collectVisible(&out)
	return out
}

func (n *treeNode) collectVisible(out *[]*treeNode) {
	*out = append(*out, n)
	if !n.expanded {
		return
	}
	for _, ch := range n.children {
		ch.collectVisible(out)
	}
}

// ─── filter engine ─────────────────────────────────────────────────────────

type filterSet struct {
	textSearch string
	typeFilter string
	statusFilter string
	showIdle   bool
	timeWindow time.Duration
	sortBy     string
	sortDesc   bool
}

func filterMatch(res *resourceState, f filterSet, now int64) bool {
	if f.typeFilter != "" && res.Type != f.typeFilter {
		return false
	}
	if f.statusFilter != "" && res.Status != f.statusFilter {
		return false
	}
	if f.textSearch != "" {
		re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(f.textSearch))
		if err != nil || (!re.MatchString(res.Label) && !re.MatchString(res.ID) && !re.MatchString(res.Type)) {
			return false
		}
	}
	if !f.showIdle && res.Status == "finished" {
		if f.timeWindow <= 0 {
			return false
		}
		finishNs := res.FinishedAt
		if finishNs == 0 {
			finishNs = res.StartedAt
		}
		age := now - finishNs
		if age > f.timeWindow.Nanoseconds() {
			return false
		}
	}
	return true
}

func (c *localCache) flatResources(filters filterSet) []*resourceState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now().UnixNano()
	result := make([]*resourceState, 0, len(c.resources))
	for _, res := range c.resources {
		if filterMatch(res, filters, now) {
			result = append(result, res)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		switch filters.sortBy {
		case "type":
			if filters.sortDesc {
				return result[i].Type > result[j].Type
			}
			return result[i].Type < result[j].Type
		case "label":
			if filters.sortDesc {
				return result[i].Label > result[j].Label
			}
			return result[i].Label < result[j].Label
		case "status":
			if filters.sortDesc {
				return result[i].Status > result[j].Status
			}
			return result[i].Status < result[j].Status
		default:
			if filters.sortDesc {
				return result[i].StartedAt > result[j].StartedAt
			}
			return result[i].StartedAt < result[j].StartedAt
		}
	})
	return result
}

// ─── ANSI helpers (Monokai palette) ────────────────────────────────────────

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRev    = "\033[7m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiBlue   = "\033[34m"
	ansiPurple = "\033[35m"
	ansiCyan   = "\033[36m"
	ansiWhite  = "\033[37m"
	ansiBlack  = "\033[30m"
	ansiClear  = "\033[2J\033[H"
	ansiHide   = "\033[?25l"
	ansiShow   = "\033[?25h"
	ansiClearLn = "\033[K"
)

func colorByStatus(s string) string {
	switch s {
	case "active", "bg_worker", "running":
		return ansiGreen
	case "finished", "idle":
		return ansiDim
	case "crashed":
		return ansiRed
	case "pending":
		return ansiYellow
	default:
		return ""
	}
}

func colorByType(t string) string {
	switch t {
	case "daemon":
		return ansiBold + ansiWhite
	case "plugin":
		return ansiCyan
	case "session":
		return ansiYellow
	case "client":
		return ansiBlue
	case "executor":
		return ansiGreen
	case "bg_task":
		return ansiPurple
	case "shell":
		return ansiDim + ansiGreen
	default:
		return ansiDim
	}
}

// ─── TUI ───────────────────────────────────────────────────────────────────

type viewMode int

const (
	viewTree viewMode = iota
	viewTable
)

type tui struct {
	ctx          plugin.Context
	cache        *localCache
	view         viewMode
	filters      filterSet

	treeNodes    []*treeNode
	flatRes      []*resourceState

	cursor       int
	offset       int
	width, height int

	colSort      int
	colAsc       bool

	lastUpdate   time.Time
	eventCount   int
	searchMode   bool
	searchBuf    string
	statusMsg    string

	running      bool
	renderReq    chan struct{}
}

func newTUI(ctx plugin.Context, cache *localCache) *tui {
	t := &tui{
		ctx:       ctx,
		cache:     cache,
		view:      viewTree,
		filters:   filterSet{sortBy: "started_at"},
		renderReq: make(chan struct{}, 1),
	}
	return t
}

func (t *tui) run(streamCtx context.Context, queryClient dotfilesdv1connect.DiagnosticsQueryServiceClient) {
	keyCh := make(chan byte, 64)
	updateCh := make(chan struct{}, 1)

	go t.readKeys(keyCh)
	go t.subscribeEvents(streamCtx, queryClient, updateCh)

	t.running = true
	t.render()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for t.running {
		select {
		case key := <-keyCh:
			t.handleKey(key, keyCh)
		case <-updateCh:
			t.requestRender()
		case <-ticker.C:
			t.requestRender()
		}
	}

	fmt.Fprint(t.ctx.Stdout(), ansiShow+ansiClear+"tui-diag closed.\n")
}

func (t *tui) requestRender() {
	select {
	case t.renderReq <- struct{}{}:
	default:
	}
}

func (t *tui) readKeys(keyCh chan<- byte) {
	r := bufio.NewReader(t.ctx.Stdin())
	for {
		b, err := r.ReadByte()
		if err != nil {
			close(keyCh)
			return
		}
		keyCh <- b
	}
}

func (t *tui) subscribeEvents(ctx context.Context, client dotfilesdv1connect.DiagnosticsQueryServiceClient, updateCh chan<- struct{}) {
	stream, err := client.StreamEvents(ctx, connect.NewRequest(&dotfilesdv1.StreamEventsRequest{}))
	if err != nil {
		return
	}
	for stream.Receive() {
		evt := stream.Msg()
		t.cache.applyEvent(evt)
		t.eventCount++
		select {
		case updateCh <- struct{}{}:
		default:
		}
	}
}

func (t *tui) rebuildData() {
	t.treeNodes = t.cache.buildTree(t.filters)
	t.flatRes = t.cache.flatResources(t.filters)
	t.lastUpdate = time.Now()
}

func (t *tui) render() {
	if len(t.renderReq) > 0 {
		<-t.renderReq
	}

	t.rebuildData()

	w := t.ctx.Stdout()
	termWidth := 80
	termHeight := 24

	fmt.Fprint(w, ansiHide)

	t.renderHeader(w, termWidth)
	switch t.view {
	case viewTree:
		t.renderTreeBody(w, termWidth, termHeight-3)
	case viewTable:
		t.renderTableBody(w, termWidth, termHeight-3)
	}
	t.renderFooter(w, termWidth)

	if t.searchMode {
		t.renderSearchBar(w, termWidth)
	}

	fmt.Fprint(w, ansiShow)
}

func (t *tui) renderHeader(w io.Writer, width int) {
	tabs := fmt.Sprintf("%s [Tree] %s  %s [Table] %s",
		map[bool]string{true: ansiRev}[t.view == viewTree],
		map[bool]string{true: ansiReset}[t.view == viewTree],
		map[bool]string{true: ansiRev}[t.view == viewTable],
		map[bool]string{true: ansiReset}[t.view == viewTable])

	title := ansiBold + " tui-diag v1.0.0 " + ansiReset
	info := fmt.Sprintf("  \033[2m%d events  %d nodes\033[0m", t.eventCount, len(t.cache.resources))

	right := tabs
	pad := width - visibleLen(title) - visibleLen(right) - visibleLen(info)
	if pad < 1 {
		pad = 1
	}

	fmt.Fprint(w, title+info+strings.Repeat(" ", pad)+right+"\n")
	fmt.Fprint(w, strings.Repeat("\u2500", width)+"\n")
}

func (t *tui) renderFooter(w io.Writer, width int) {
	fmt.Fprint(w, strings.Repeat("\u2500", width)+"\n")

	var parts []string
	if t.filters.typeFilter != "" {
		parts = append(parts, ansiYellow+"type="+t.filters.typeFilter+ansiReset)
	}
	if t.filters.statusFilter != "" {
		parts = append(parts, ansiYellow+"status="+t.filters.statusFilter+ansiReset)
	}
	if t.filters.textSearch != "" {
		parts = append(parts, ansiYellow+"/"+t.filters.textSearch+ansiReset)
	}
	if t.filters.showIdle {
		parts = append(parts, ansiDim+"show_idle"+ansiReset)
	}

	var filterStr string
	if len(parts) > 0 {
		filterStr = "Filter: " + strings.Join(parts, " ")
	} else {
		filterStr = ansiDim + "no filters" + ansiReset
	}

	updated := time.Since(t.lastUpdate).Round(100 * time.Millisecond).String()
	status := fmt.Sprintf("\033[2m%s  updated %s ago\033[0m", filterStr, updated)

	keys := ansiDim + "F1-hlp /-srch Tab-view \u2191/\u2193-nav q-quit" + ansiReset
	pad := width - visibleLen(status) - visibleLen(keys)
	if pad < 0 {
		pad = 0
	}

	fmt.Fprint(w, status+strings.Repeat(" ", pad)+keys+"\n")
}

func (t *tui) renderSearchBar(w io.Writer, width int) {
	fmt.Fprint(w, "Search: "+t.searchBuf+"\033[K\n")
}

// ─── tree view ─────────────────────────────────────────────────────────────

func (t *tui) renderTreeBody(w io.Writer, width, height int) {
	lines := buildTreeLines(t.treeNodes)

	if height <= 0 {
		return
	}

	if t.cursor >= len(lines) {
		t.cursor = len(lines) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}

	if t.offset > t.cursor {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+height {
		t.offset = t.cursor - height + 1
	}
	if t.offset+height > len(lines) {
		t.offset = len(lines) - height
	}
	if t.offset < 0 {
		t.offset = 0
	}

	end := t.offset + height
	if end > len(lines) {
		end = len(lines)
	}

	for i := t.offset; i < end; i++ {
		line := t.formatTreeRow(lines[i], width)
		if i == t.cursor {
			fmt.Fprint(w, ansiRev+line+ansiReset+"\033[K\n")
		} else {
			fmt.Fprint(w, line+"\033[K\n")
		}
	}

	for i := end - t.offset; i < height; i++ {
		fmt.Fprint(w, "\033[K\n")
	}

	if len(lines) == 0 {
		fmt.Fprint(w, ansiDim+"  (no matching resources)"+ansiReset+"\033[K\n")
	}
}

func flattenVisible(roots []*treeNode) []*treeNode {
	var out []*treeNode
	for _, r := range roots {
		r.collectVisible(&out)
	}
	return out
}

type treeLine struct {
	node     *treeNode
	prefix   string
	isLast   bool
}

func buildTreeLines(roots []*treeNode) []treeLine {
	var out []treeLine
	var walk func(nodes []*treeNode, prefix string)
	walk = func(nodes []*treeNode, prefix string) {
		for i, n := range nodes {
			isLast := i == len(nodes)-1
			conn := "\u251c\u2500\u2500 "
			if isLast {
				conn = "\u2514\u2500\u2500 "
			}
			childPrefix := prefix
			if n.depth > 0 {
				childPrefix = prefix + map[bool]string{true: "    ", false: "\u2502   "}[isLast]
			}
			out = append(out, treeLine{node: n, prefix: prefix + conn, isLast: isLast})
			if n.expanded && len(n.children) > 0 {
				walk(n.children, childPrefix)
			}
		}
	}
	walk(roots, "")
	return out
}

func (t *tui) formatTreeRow(tl treeLine, width int) string {
	n := tl.node
	expander := " "
	if len(n.children) > 0 {
		if n.expanded {
			expander = "\u25bc"
		} else {
			expander = "\u25b6"
		}
	}

	typeColor := colorByType(n.entry.Type)

	label := typeColor + n.entry.Type + ansiReset + ":" + ansiBold + n.entry.Label + ansiReset

	var attrs []string
	if pid := n.entry.Attrs["pid"]; pid != "" {
		attrs = append(attrs, "pid="+pid)
	}
	if dur := n.entry.Attrs["duration"]; dur != "" {
		attrs = append(attrs, dur)
	}
	if activeStr := n.entry.Attrs["running_for"]; activeStr != "" {
		attrs = append(attrs, ansiDim+"up "+activeStr+ansiReset)
	}
	attrStr := strings.Join(attrs, " ")

	line := tl.prefix + expander + " " + label
	if n.entry.Type != "daemon" {
		statusColor := colorByStatus(n.entry.Status)
		line += "  " + statusColor + n.entry.Status + ansiReset
	}
	if attrStr != "" {
		line += "  " + attrStr
	}

	return truncatePad(line, width)
}

// ─── table view ────────────────────────────────────────────────────────────

var tableHeaders = []struct {
	label string
	key   string
	width int
}{
	{"TYPE", "type", 10},
	{"LABEL", "label", 24},
	{"STATUS", "status", 12},
	{"STARTED", "started", 14},
	{"DURATION", "duration", 12},
}

func (t *tui) renderTableBody(w io.Writer, width, height int) {
	resources := t.flatRes
	headerRows := 2
	bodyHeight := height - headerRows

	if bodyHeight < 0 {
		bodyHeight = 0
	}

	if t.cursor >= len(resources) {
		t.cursor = len(resources) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}

	if t.offset > t.cursor {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+bodyHeight {
		t.offset = t.cursor - bodyHeight + 1
	}
	if t.offset+bodyHeight > len(resources) {
		t.offset = len(resources) - bodyHeight
	}
	if t.offset < 0 {
		t.offset = 0
	}

	// Header
	fmt.Fprint(w, ansiBold)
	col := 0
	for _, h := range tableHeaders {
		sep := "\u2502"
		if col > 0 {
			fmt.Fprint(w, "  "+sep+"  ")
			col += 4
		}
		label := h.label
		if t.colSort+1 == colIndex(h.key) {
			if t.colAsc {
				label = "\u25b2 " + label
			} else {
				label = "\u25bc " + label
			}
		}
		fmt.Fprint(w, truncatePad(label, h.width))
		col += h.width
	}
	fmt.Fprint(w, ansiReset+"\n")

	// Separator
	col = 0
	for i, h := range tableHeaders {
		sep := "\u2502"
		if i > 0 {
			fmt.Fprint(w, "\u2500\u2500\u2500\u2500\u2500"+sep)
			col += 6
		}
		fmt.Fprint(w, strings.Repeat("\u2500", h.width))
		col += h.width
	}
	fmt.Fprint(w, "\n")

	// Body
	end := t.offset + bodyHeight
	if end > len(resources) {
		end = len(resources)
	}
	for i := t.offset; i < end; i++ {
		r := resources[i]
		line := t.formatTableRow(r)
		if i == t.cursor {
			fmt.Fprint(w, ansiRev+line+ansiReset+"\033[K\n")
		} else {
			fmt.Fprint(w, line+"\033[K\n")
		}
	}

	fill := bodyHeight - (end - t.offset)
	if fill > 0 {
		for i := 0; i < fill; i++ {
			fmt.Fprint(w, "\033[K\n")
		}
	}

	if len(resources) == 0 {
		fmt.Fprint(w, ansiDim+"  (no matching resources)"+ansiReset+"\033[K\n")
	}
}

func colIndex(key string) int {
	for i, h := range tableHeaders {
		if h.key == key {
			return i
		}
	}
	return -1
}

func (t *tui) formatTableRow(r *resourceState) string {
	started := time.Unix(0, r.StartedAt)
	startedStr := timeSince(started)

	var durStr string
	if r.DurationNs > 0 {
		durStr = time.Duration(r.DurationNs).Round(time.Millisecond).String()
	} else if r.Status == "active" {
		durStr = timeSince(started)
	} else {
		durStr = "\u2014"
	}

	typeColor := colorByType(r.Type)
	statusColor := colorByStatus(r.Status)

	parts := []string{
		typeColor + truncatePad(r.Type, 10) + ansiReset,
		ansiBold + truncatePad(r.Label, 24) + ansiReset,
		statusColor + truncatePad(r.Status, 12) + ansiReset,
		ansiDim + truncatePad(startedStr, 14) + ansiReset,
		ansiDim + truncatePad(durStr, 12) + ansiReset,
	}

	return strings.Join(parts, "  \u2502  ")
}

// ─── keyboard handling ─────────────────────────────────────────────────────

func (t *tui) handleKey(key byte, keyCh <-chan byte) {
	if t.searchMode {
		t.handleSearchKey(key, keyCh)
		return
	}

	switch key {
	case 'q', 'Q':
		t.running = false
	case '\t':
		t.view = map[viewMode]viewMode{viewTree: viewTable, viewTable: viewTree}[t.view]
		t.cursor = 0
		t.offset = 0
		t.render()
	case 'j', 0: // down arrow = 0x0a(\n) in raw mode; also 'j'
		t.cursor++
		t.render()
	case 'k': // up
		t.cursor--
		if t.cursor < 0 {
			t.cursor = 0
		}
		t.render()
	case '/':
		t.searchMode = true
		t.searchBuf = ""
		t.renderSearchBar(t.ctx.Stdout(), 80)
	case 'h', 'H':
		t.showHelp()
	case 'i', 'I':
		t.filters.showIdle = !t.filters.showIdle
		t.cursor = 0
		t.offset = 0
		t.render()
	case 't', 'T':
		t.cycleTypeFilter()
		t.render()
	case 's', 'S':
		t.cycleStatusFilter()
		t.render()
	case ' ', 0x0d, 0x0a: // space or enter — expand/collapse in tree
		t.toggleExpand()
		t.render()
	case 27: // escape sequence (arrow keys send \033[A etc)
		t.handleEscapeSeq(keyCh)
	default:
	}
}

func (t *tui) handleEscapeSeq(keyCh <-chan byte) {
	// After \033, read next two bytes for arrow keys
	timer := time.NewTimer(50 * time.Millisecond)
	defer timer.Stop()

	var buf []byte
	for i := 0; i < 2; i++ {
		select {
		case b, ok := <-keyCh:
			if !ok {
				return
			}
			buf = append(buf, b)
		case <-timer.C:
			return
		}
	}

	if len(buf) == 2 && buf[0] == '[' {
		switch buf[1] {
		case 'A': // up
			t.cursor--
			if t.cursor < 0 {
				t.cursor = 0
			}
			t.render()
		case 'B': // down
			t.cursor++
			t.render()
		case 'C': // right — expand
			t.expandNode()
			t.render()
		case 'D': // left — collapse
			t.collapseNode()
			t.render()
		}
	}
}

func (t *tui) handleSearchKey(key byte, keyCh <-chan byte) {
	switch key {
	case 0x0d, 0x0a: // enter
		t.filters.textSearch = t.searchBuf
		t.searchMode = false
		t.cursor = 0
		t.offset = 0
		t.render()
	case 27: // escape
		t.searchMode = false
		t.render()
	case 127, '\b': // backspace
		if len(t.searchBuf) > 0 {
			t.searchBuf = t.searchBuf[:len(t.searchBuf)-1]
		}
		t.renderSearchBar(t.ctx.Stdout(), 80)
	default:
		if key >= 32 && key < 127 {
			t.searchBuf += string(key)
			t.renderSearchBar(t.ctx.Stdout(), 80)
		}
	}
}

func (t *tui) toggleExpand() {
	if t.view == viewTree {
		lines := buildTreeLines(t.treeNodes)
		if t.cursor >= 0 && t.cursor < len(lines) {
			n := lines[t.cursor].node
			if len(n.children) > 0 {
				n.expanded = !n.expanded
			}
		}
	}
}

func (t *tui) expandNode() {
	if t.view == viewTree {
		lines := buildTreeLines(t.treeNodes)
		if t.cursor >= 0 && t.cursor < len(lines) {
			lines[t.cursor].node.expanded = true
		}
	}
}

func (t *tui) collapseNode() {
	if t.view == viewTree {
		lines := buildTreeLines(t.treeNodes)
		if t.cursor >= 0 && t.cursor < len(lines) {
			lines[t.cursor].node.expanded = false
		}
	}
}

func (t *tui) cycleTypeFilter() {
	types := []string{"", "daemon", "plugin", "session", "client", "executor", "bg_task"}
	idx := slices.Index(types, t.filters.typeFilter)
	if idx < 0 {
		idx = 0
	}
	idx = (idx + 1) % len(types)
	t.filters.typeFilter = types[idx]
}

func (t *tui) cycleStatusFilter() {
	statuses := []string{"", "active", "finished", "crashed", "pending"}
	idx := slices.Index(statuses, t.filters.statusFilter)
	if idx < 0 {
		idx = 0
	}
	idx = (idx + 1) % len(statuses)
	t.filters.statusFilter = statuses[idx]
}

func (t *tui) showHelp() {
	w := t.ctx.Stdout()
	fmt.Fprint(w, ansiClear+ansiBold+" tui-diag help"+ansiReset+"\n\n")
	fmt.Fprint(w, "  Tab       switch between Tree / Table view\n")
	fmt.Fprint(w, "  \u2191/\u2192      navigate (also j/k)\n")
	fmt.Fprint(w, "  \u2192/\u2190      expand/collapse node (tree view)\n")
	fmt.Fprint(w, "  Space     toggle expand/collapse\n")
	fmt.Fprint(w, "  /         search by label/ID/type\n")
	fmt.Fprint(w, "  Enter     confirm search\n")
	fmt.Fprint(w, "  Esc       cancel search\n")
	fmt.Fprint(w, "  t         cycle type filter\n")
	fmt.Fprint(w, "  s         cycle status filter\n")
	fmt.Fprint(w, "  i         toggle show idle\n")
	fmt.Fprint(w, "  q         quit\n")
	fmt.Fprint(w, "\n"+ansiDim+"Press any key to return"+ansiReset+"\n")

	r := bufio.NewReader(t.ctx.Stdin())
	r.ReadByte()
	t.render()
}

// ─── utilities ─────────────────────────────────────────────────────────────

func visibleLen(s string) int {
	n := 0
	inEscape := false
	for _, c := range s {
		if c == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if c == 'm' || (c >= '0' && c <= '9') || c == ';' || c == '[' {
				if c == 'm' {
					inEscape = false
				}
				continue
			}
			inEscape = false
		}
		n++
	}
	return n
}

func truncatePad(s string, max int) string {
	v := visibleLen(s)
	if v > max {
		// Truncate visible characters
		out := make([]byte, 0, len(s))
		count := 0
		inEsc := false
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c == '\033' {
				inEsc = true
				out = append(out, c)
				continue
			}
			if inEsc {
				out = append(out, c)
				if c == 'm' {
					inEsc = false
				}
				continue
			}
			if count >= max {
				break
			}
			out = append(out, c)
			count++
		}
		return string(out) + strings.Repeat(" ", max-count)
	}
	return s + strings.Repeat(" ", max-v)
}

func timeSince(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

// ─── diagnostics query client ──────────────────────────────────────────────

func newQueryClient() dotfilesdv1connect.DiagnosticsQueryServiceClient {
	addr := os.Getenv("EXECUTION_CONTEXT_URL")
	if addr == "" {
		addr = "http://127.0.0.1:9105"
	}
	return dotfilesdv1connect.NewDiagnosticsQueryServiceClient(
		http.DefaultClient,
		addr,
	)
}

// ─── plugin service ────────────────────────────────────────────────────────

type tuiDiagSvc struct {
	cache *localCache
}

func (s *tuiDiagSvc) Watch(ctx context.Context, req *connect.Request[pb.WatchRequest]) (*connect.Response[pb.WatchResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("no plugin context"))
	}

	r := req.Msg

	queryClient := newQueryClient()

	treeResp, err := queryClient.QueryTree(ctx, connect.NewRequest(&dotfilesdv1.QueryTreeRequest{
		ShowIdle: r.ShowIdle,
	}))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("query tree: %w", err))
	}

	cache := newCache()
	populateCacheFromTree(cache, treeResp.Msg.Root)

	tui := newTUI(pc, cache)
	if r.InitialTypeFilter != "" {
		tui.filters.typeFilter = r.InitialTypeFilter
	}
	if r.InitialStatusFilter != "" {
		tui.filters.statusFilter = r.InitialStatusFilter
	}
	tui.filters.showIdle = r.ShowIdle

	tui.run(ctx, queryClient)

	return connect.NewResponse(&pb.WatchResponse{}), nil
}

func populateCacheFromTree(cache *localCache, root *dotfilesdv1.DiagNode) {
	cache.mu.Lock()
	var walk func(n *dotfilesdv1.DiagNode, parentID string)
	walk = func(n *dotfilesdv1.DiagNode, parentID string) {
		if n == nil {
			return
		}
		id := n.Type + ":" + n.Label

		startedAt := time.Now().UnixNano()
		if nsStr := n.Attrs["running_for_ns"]; nsStr != "" {
			if ns, err := parseNanoseconds(nsStr); err == nil {
				startedAt = time.Now().UnixNano() - ns
			}
		}
		finishedAt := int64(0)
		var durationNs int64
		if nsStr := n.Attrs["duration_ns"]; nsStr != "" {
			if ns, err := parseNanoseconds(nsStr); err == nil {
				durationNs = ns
			}
		}

		res := &resourceState{
			ID:         id,
			Type:       n.Type,
			Label:      n.Label,
			Status:     n.Status,
			ParentID:   parentID,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			DurationNs: durationNs,
			Attrs:      make(map[string]string),
		}
		for k, v := range n.Attrs {
			res.Attrs[k] = v
		}
		cache.resources[id] = res

		for _, child := range n.Children {
			walk(child, id)
		}
	}
	walk(root, "")
	cache.mu.Unlock()
}

func parseNanoseconds(s string) (int64, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("empty string")
	}
	return strconv.ParseInt(s, 10, 64)
}

// ─── main ──────────────────────────────────────────────────────────────────

func main() {
	cache := newCache()
	svc := &tuiDiagSvc{cache: cache}
	path, handler := tuidiagconnect.NewTuiDiagServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "tui-diag",
		DisplayName: "TUI Diagnostics",
		Version:     "1.0.0",
		Description: "Interactive htop-like diagnostics browser for the daemon runtime state tree",
		DocsProto:   nil,
		Services: []plugin.Service{
			{
				Name:               "tuidiag.TuiDiagService",
				Description:        "Interactive TUI diagnostics browser",
				Path:               path,
				Handler:            handler,
				PluginAccessible:   false,
				InteractiveMethods: []string{"Watch"},
			},
		},
	})
}
