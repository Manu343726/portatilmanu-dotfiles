package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"dotfilesd/plugin"
	"plugins/zerotier/api/zerotiercentral"
	pb "plugins/zerotier/proto/zerotier"
	"plugins/zerotier/proto/zerotier/zerotierconnect"

	"connectrpc.com/connect"
)

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

	api, token, err := newZTClient(pc)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(token)

	resp, err := api.GetNetworkListWithResponse(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("zerotier api error: %w", err))
	}
	if resp.JSON200 == nil {
		return nil, apiError(resp.HTTPResponse)
	}

	networks := make([]*pb.Network, 0, len(*resp.JSON200))
	for _, n := range *resp.JSON200 {
		name := ""
		if n.Config != nil && n.Config.Name != nil {
			name = *n.Config.Name
		}
		networks = append(networks, &pb.Network{
			Id:           safeStr(n.Id),
			Name:         name,
			Description:  safeStr(n.Description),
			MemberCount:  int32(safeInt(n.TotalMemberCount)),
			CreationTime: safeInt64(n.Clock),
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

	api, token, err := newZTClient(pc)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(token)

	resp, err := api.GetNetworkMemberListWithResponse(ctx, networkID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("zerotier api error: %w", err))
	}
	if resp.JSON200 == nil {
		return nil, apiError(resp.HTTPResponse)
	}

	nowMs := time.Now().UnixMilli()
	members := make([]*pb.Member, 0, len(*resp.JSON200))
	for _, m := range *resp.JSON200 {
		lastOnline := safeInt64(m.LastOnline)
		online := lastOnline > 0 && (nowMs-lastOnline) < 300_000 // 5 min in ms
		authorized := false
		var ipAssignments []string
		if m.Config != nil {
			if m.Config.Authorized != nil {
				authorized = *m.Config.Authorized
			}
			if m.Config.IpAssignments != nil {
				ipAssignments = *m.Config.IpAssignments
			}
		}
		members = append(members, &pb.Member{
			Id:              safeStr(m.Id),
			NodeId:          safeStr(m.NodeId),
			Name:            safeStr(m.Name),
			Description:     safeStr(m.Description),
			Authorized:      authorized,
			IpAssignments:   ipAssignments,
			LastOnline:      lastOnline,
			Online:          online,
			PhysicalAddress: safeStr(m.PhysicalAddress),
			ClientVersion:   safeStr(m.ClientVersion),
		})
	}

	filter := req.Msg.GetFilter()
	members = filterMembers(members, filter.GetStatus(), filter.GetNameSubstring())

	if pc.RenderOutput() {
		if len(members) == 0 {
			fmt.Fprintln(pc.Stdout(), "No members match the current filters.")
		} else {
			display := req.Msg.GetDisplay()
			cols := resolveColumns(display.GetFields())
			switch display.GetFormat() {
			case pb.OutputFormat_OUTPUT_FORMAT_RAW:
				printMembersRaw(pc, members, networkID, cols)
			default:
				printMembersTable(pc, members, networkID, cols)
			}
		}
	}

	return connect.NewResponse(&pb.ListMembersResponse{Members: members}), nil
}

// ── ZeroTier API client ─────────────────────────────────────────────────────

func newZTClient(pc plugin.Context) (*zerotiercentral.ClientWithResponses, []byte, error) {
	token, err := pc.GetSecret("api_token")
	if err != nil {
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("zerotier api_token not configured: %w.\n\n  Run: echo 'zerotier:\n  api_token: \"<token>\"' >> ~/.config/dotfilesd/secrets.yaml\n  Then: systemctl --user restart dotfilesd", err))
	}

	ztAPI, err := zerotiercentral.NewClientWithResponses(
		"https://api.zerotier.com/api/v1",
		zerotiercentral.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "bearer "+string(token))
			return nil
		}),
	)
	if err != nil {
		zeroBytes(token)
		return nil, nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create api client: %w", err))
	}

	return ztAPI, token, nil
}

func apiError(resp *http.Response) error {
	if resp == nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("empty response from ZeroTier API"))
	}
	return connect.NewError(connect.CodeInternal,
		fmt.Errorf("zerotier api error (HTTP %d)", resp.StatusCode))
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func safeStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func safeInt64(i *int64) int64 {
	if i == nil {
		return 0
	}
	return *i
}

func safeInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

// ── Filter ───────────────────────────────────────────────────────────────────

func filterMembers(members []*pb.Member, status pb.MemberStatus, nameSubstring string) []*pb.Member {
	out := make([]*pb.Member, 0, len(members))
	for _, m := range members {
		switch status {
		case pb.MemberStatus_MEMBER_STATUS_ONLINE:
			if !m.Online {
				continue
			}
		case pb.MemberStatus_MEMBER_STATUS_OFFLINE:
			if m.Online {
				continue
			}
		case pb.MemberStatus_MEMBER_STATUS_AUTHORIZED:
			if !m.Authorized {
				continue
			}
		case pb.MemberStatus_MEMBER_STATUS_UNAUTHORIZED:
			if m.Authorized {
				continue
			}
		}
		if nameSubstring != "" && !strings.Contains(strings.ToLower(m.Name), strings.ToLower(nameSubstring)) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// ── Column resolution ────────────────────────────────────────────────────────

type colHint struct {
	Label string
	Width int
	Value func(m *pb.Member) string
}

var allColumns = map[pb.Column]colHint{
	pb.Column_COLUMN_NODE_ID:          {"Node ID", 16, func(m *pb.Member) string { return m.NodeId }},
	pb.Column_COLUMN_NAME:             {"Name", 22, func(m *pb.Member) string { return m.Name }},
	pb.Column_COLUMN_IP:               {"IPs", 18, func(m *pb.Member) string { return strings.Join(m.IpAssignments, ", ") }},
	pb.Column_COLUMN_STATUS:           {"Status", 12, nil},
	pb.Column_COLUMN_DESCRIPTION:      {"Description", 30, func(m *pb.Member) string { return m.Description }},
	pb.Column_COLUMN_VERSION:          {"Version", 10, func(m *pb.Member) string { return m.ClientVersion }},
	pb.Column_COLUMN_PHYSICAL_ADDRESS: {"Address", 22, func(m *pb.Member) string { return m.PhysicalAddress }},
}

var defaultColumns = []pb.Column{
	pb.Column_COLUMN_NODE_ID,
	pb.Column_COLUMN_NAME,
	pb.Column_COLUMN_IP,
	pb.Column_COLUMN_STATUS,
}

func resolveColumns(fields []pb.Column) []pb.Column {
	if len(fields) == 0 {
		return defaultColumns
	}
	if len(fields) == 1 && fields[0] == pb.Column_COLUMN_UNSPECIFIED {
		return defaultColumns
	}
	return fields
}

// ── Output helpers ───────────────────────────────────────────────────────────

func statusLabel(pc plugin.Context, m *pb.Member) string {
	if !m.Authorized {
		return pc.Dimf("unauthorized")
	}
	if m.Online {
		return pc.Greenf("online")
	}
	return pc.Redf("offline")
}

func printMembersTable(pc plugin.Context, members []*pb.Member, networkID string, cols []pb.Column) {
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
			if c == pb.Column_COLUMN_STATUS {
				row[i] = statusLabel(pc, m)
			} else {
				row[i] = allColumns[c].Value(m)
			}
		}
		fmt.Fprintf(&buf, colFmt, row...)
		buf.WriteByte('\n')
	}

	fmt.Fprint(pc.Stdout(), buf.String())
}

func printMembersRaw(pc plugin.Context, members []*pb.Member, networkID string, cols []pb.Column) {
	fmt.Fprintf(pc.Stdout(), "network_id: %s  members: %d\n\n", networkID, len(members))
	for _, m := range members {
		for _, c := range cols {
			h := allColumns[c]
			if c == pb.Column_COLUMN_STATUS {
				fmt.Fprintf(pc.Stdout(), "%s: %s\n", h.Label, statusLabel(pc, m))
			} else {
				fmt.Fprintf(pc.Stdout(), "%s: %s\n", h.Label, h.Value(m))
			}
		}
		fmt.Fprintln(pc.Stdout())
	}
}

func resolveSingleNetwork(ctx context.Context, pc plugin.Context) (string, error) {
	api, token, err := newZTClient(pc)
	if err != nil {
		return "", err
	}
	defer zeroBytes(token)

	resp, err := api.GetNetworkListWithResponse(ctx)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal,
			fmt.Errorf("zerotier api error: %w", err))
	}
	if resp.JSON200 == nil {
		return "", apiError(resp.HTTPResponse)
	}

	switch len(*resp.JSON200) {
	case 0:
		return "", connect.NewError(connect.CodeNotFound,
			fmt.Errorf("no ZeroTier networks found"))
	case 1:
		n := (*resp.JSON200)[0]
		return safeStr(n.Id), nil
	default:
		return "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("multiple networks found (%d); specify --network-id", len(*resp.JSON200)))
	}
}

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
