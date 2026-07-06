package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"dotfilesd/plugin"
	pb "plugins/zerotier/proto/zerotier"
	"plugins/zerotier/proto/zerotier/zerotierconnect"

	"connectrpc.com/connect"
)

const centralAPI = "https://api.zerotier.com/api/v1"

// ztNetwork is the relevant portion of ZeroTier Central's /network response.
type ztNetwork struct {
	ID               string `json:"id"`
	TotalMemberCount int    `json:"totalMemberCount"`
	CreationTime     int64  `json:"creationTime"`
	Config           struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"config"`
}

// ztMember is the relevant portion of ZeroTier Central's /network/{id}/member response.
type ztMember struct {
	ID              string `json:"id"`
	NodeID          string `json:"nodeId"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	PhysicalAddress string `json:"physicalAddress"`
	ClientVersion   string `json:"clientVersion"`
	LastOnline      int64  `json:"lastOnline"`
	Config          struct {
		Authorized    bool     `json:"authorized"`
		IPAssignments []string `json:"ipAssignments"`
	} `json:"config"`
}

// zeroTierService implements the ZeroTierService RPCs.
type zeroTierService struct{}

func (s *zeroTierService) ListNetworks(
	ctx context.Context,
	req *connect.Request[pb.ListNetworksRequest],
) (*connect.Response[pb.ListNetworksResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("no daemon context"))
	}

	token, err := pc.GetSecret("api_token")
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("zerotier api_token not configured: %w.\n\n  Run: echo 'zerotier:\n  api_token: \"<token>\"' >> ~/.config/dotfilesd/secrets.yaml\n  Then: systemctl --user restart dotfilesd", err))
	}
	defer zeroBytes(token)

	raw, err := ztGet[[]ztNetwork](ctx, "/network", string(token))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("zerotier api error: %w", err))
	}

	networks := make([]*pb.Network, 0, len(raw))
	for _, n := range raw {
		networks = append(networks, &pb.Network{
			Id:          n.ID,
			Name:        n.Config.Name,
			Description: n.Config.Description,
			MemberCount: int32(n.TotalMemberCount),
			CreationTime: n.CreationTime,
		})
	}

	if pc.RenderOutput() {
		if len(networks) == 0 {
			fmt.Fprintln(pc.Stdout(), "No ZeroTier networks found.")
		} else {
			fmt.Fprintf(pc.Stdout(), "%-22s  %-30s  %s\n", "Network ID", "Name", "Members")
			for _, n := range networks {
				fmt.Fprintf(pc.Stdout(), "%-22s  %-30s  %d\n", n.Id, n.Name, n.MemberCount)
			}
		}
	}

	return connect.NewResponse(&pb.ListNetworksResponse{Networks: networks}), nil
}

func (s *zeroTierService) ListMembers(
	ctx context.Context,
	req *connect.Request[pb.ListMembersRequest],
) (*connect.Response[pb.ListMembersResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("no daemon context"))
	}

	networkID := req.Msg.NetworkId
	if networkID == "" {
		id, err := resolveSingleNetwork(ctx, pc)
		if err != nil {
			return nil, err
		}
		networkID = id
	}

	token, err := pc.GetSecret("api_token")
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("zerotier api_token not configured: %w.\n\n  Run: echo 'zerotier:\n  api_token: \"<token>\"' >> ~/.config/dotfilesd/secrets.yaml\n  Then: systemctl --user restart dotfilesd", err))
	}
	defer zeroBytes(token)

	path := fmt.Sprintf("/network/%s/member", networkID)
	raw, err := ztGet[[]ztMember](ctx, path, string(token))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("zerotier api error: %w", err))
	}

	now := time.Now().Unix()
	members := make([]*pb.Member, 0, len(raw))
	for _, m := range raw {
		online := m.LastOnline > 0 && (now-m.LastOnline) < 300
		members = append(members, &pb.Member{
			Id:              m.ID,
			NodeId:          m.NodeID,
			Name:            m.Name,
			Description:     m.Description,
			Authorized:      m.Config.Authorized,
			IpAssignments:   m.Config.IPAssignments,
			LastOnline:      m.LastOnline,
			Online:          online,
			PhysicalAddress: m.PhysicalAddress,
			ClientVersion:   m.ClientVersion,
		})
	}

	members = filterMembers(members, req.Msg.Status, req.Msg.NameFilter)

	if pc.RenderOutput() {
		if len(members) == 0 {
			fmt.Fprintln(pc.Stdout(), "No members match the current filters.")
		} else {
			cols := parseColumns(req.Msg.Fields)
			switch req.Msg.Output {
			case "raw":
				printMembersRaw(pc, members, networkID, cols)
			default:
				printMembersTable(pc, members, networkID, cols)
			}
		}
	}

	return connect.NewResponse(&pb.ListMembersResponse{Members: members}), nil
}

// filterMembers applies status and name filters to the member list.
func filterMembers(members []*pb.Member, status, nameFilter string) []*pb.Member {
	out := make([]*pb.Member, 0, len(members))
	for _, m := range members {
		switch strings.ToLower(status) {
		case "online":
			if !m.Online || !m.Authorized {
				continue
			}
		case "offline":
			if m.Online || !m.Authorized {
				continue
			}
		case "authorized":
			if !m.Authorized {
				continue
			}
		case "unauthorized":
			if m.Authorized {
				continue
			}
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(m.Name), strings.ToLower(nameFilter)) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// colHints defines column metadata for table output.
type colHint struct {
	Label string
	Width int
	Value func(m *pb.Member) string
}

var allColumns = map[string]colHint{
	"node_id":          {"Node ID", 16, func(m *pb.Member) string { return m.NodeId }},
	"name":             {"Name", 22, func(m *pb.Member) string { return m.Name }},
	"ip":               {"IPs", 18, func(m *pb.Member) string { return strings.Join(m.IpAssignments, ", ") }},
	"status":           {"Status", 12, nil},
	"description":      {"Description", 30, func(m *pb.Member) string { return m.Description }},
	"version":          {"Version", 10, func(m *pb.Member) string { return m.ClientVersion }},
	"physical_address": {"Address", 22, func(m *pb.Member) string { return m.PhysicalAddress }},
}

var defaultCols = []string{"node_id", "name", "ip", "status"}

// parseColumns parses the comma-separated fields parameter into column names.
func parseColumns(fields string) []string {
	if fields == "" {
		return defaultCols
	}
	if fields == "all" {
		return []string{"node_id", "name", "ip", "status", "description", "version", "physical_address"}
	}
	var cols []string
	for _, f := range strings.Split(fields, ",") {
		f = strings.TrimSpace(f)
		if _, ok := allColumns[f]; ok {
			cols = append(cols, f)
		}
	}
	if len(cols) == 0 {
		return defaultCols
	}
	return cols
}

// statusLabel returns the colored status label for a member.
func statusLabel(pc plugin.Context, m *pb.Member) string {
	if !m.Authorized {
		return pc.Dimf("unauthorized")
	}
	if m.Online {
		return pc.Greenf("online")
	}
	return pc.Redf("offline")
}

// printMembersTable prints members as a formatted table.
func printMembersTable(pc plugin.Context, members []*pb.Member, networkID string, cols []string) {
	onlineCount := 0
	for _, m := range members {
		if m.Online {
			onlineCount++
		}
	}

	formats := make([]string, len(cols))
	headers := make([]string, len(cols))
	for i, c := range cols {
		h := allColumns[c]
		formats[i] = fmt.Sprintf("%%-%ds  ", h.Width)
		headers[i] = h.Label
	}
	colFmt := strings.Join(formats, "")

	var buf strings.Builder
	fmt.Fprintf(&buf, "Network: %s  (%d members, %d online)\n\n", networkID, len(members), onlineCount)

	headerArgs := make([]any, len(headers))
	for i, h := range headers {
		headerArgs[i] = h
	}
	fmt.Fprintf(&buf, colFmt, headerArgs...)
	buf.WriteByte('\n')

	for _, m := range members {
		row := make([]any, len(cols))
		for i, c := range cols {
			h := allColumns[c]
			if c == "status" {
				row[i] = statusLabel(pc, m)
			} else {
				row[i] = h.Value(m)
			}
		}
		fmt.Fprintf(&buf, colFmt, row...)
		buf.WriteByte('\n')
	}

	fmt.Fprint(pc.Stdout(), buf.String())
}

// printMembersRaw prints members as key:value blocks.
func printMembersRaw(pc plugin.Context, members []*pb.Member, networkID string, cols []string) {
	fmt.Fprintf(pc.Stdout(), "network_id: %s  members: %d\n\n", networkID, len(members))
	for _, m := range members {
		for _, c := range cols {
			h := allColumns[c]
			if c == "status" {
				fmt.Fprintf(pc.Stdout(), "%s: %s\n", h.Label, statusLabel(pc, m))
			} else {
				fmt.Fprintf(pc.Stdout(), "%s: %s\n", h.Label, h.Value(m))
			}
		}
		fmt.Fprintln(pc.Stdout())
	}
}

// resolveSingleNetwork fetches the network list and returns the single network ID.
// Returns an error if there are zero or multiple networks.
func resolveSingleNetwork(ctx context.Context, pc plugin.Context) (string, error) {
	token, err := pc.GetSecret("api_token")
	if err != nil {
		return "", connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("zerotier api_token not configured: %w", err))
	}
	defer zeroBytes(token)

	raw, err := ztGet[[]ztNetwork](ctx, "/network", string(token))
	if err != nil {
		return "", connect.NewError(connect.CodeInternal,
			fmt.Errorf("zerotier api error: %w", err))
	}

	switch len(raw) {
	case 0:
		return "", connect.NewError(connect.CodeNotFound,
			fmt.Errorf("no ZeroTier networks found"))
	case 1:
		return raw[0].ID, nil
	default:
		return "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("multiple networks found (%d); specify --network-id", len(raw)))
	}
}

// ztGet performs an authenticated GET request to the ZeroTier Central API.
func ztGet[T any](ctx context.Context, path, token string) (T, error) {
	var zero T
	url := centralAPI + path
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return zero, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zero, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("api error (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

// zeroBytes clears a byte slice.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func main() {
	svc := &zeroTierService{}
	path, handler := zerotierconnect.NewZeroTierServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "zerotier",
		DisplayName: "ZeroTier",
		Version:     "1.0.0",
		Description: "Query ZeroTier Central API for network members and IPs",
		Services: []plugin.Service{
			{
				Name:             "zerotier.ZeroTierService",
				Description:      "ZeroTier Central API — list networks and members",
				Path:             path,
				Handler:          handler,
				PluginAccessible: true,
			},
		},
	})
}
