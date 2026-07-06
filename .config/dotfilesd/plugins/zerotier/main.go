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
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("network_id is required"))
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
		online := m.LastOnline > 0 && (now-m.LastOnline) < 300 // online within 5 min
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

	if pc.RenderOutput() {
		if len(members) == 0 {
			fmt.Fprintln(pc.Stdout(), "No members found for network", networkID)
		} else {
			onlineCount := 0
			for _, m := range members {
				if m.Online {
					onlineCount++
				}
			}
			fmt.Fprintf(pc.Stdout(), "Network: %s  (%d members, %d online)\n\n", networkID, len(members), onlineCount)
			fmt.Fprintf(pc.Stdout(), "%-16s  %-20s  %-18s  %s\n", "Node ID", "Name", "IPs", "Status")
			for _, m := range members {
				ipStr := strings.Join(m.IpAssignments, ", ")
				status := pc.Redf("offline")
				if m.Online {
					status = pc.Greenf("online")
				}
				if !m.Authorized {
					status = pc.Dimf("unauthorized")
				}
				fmt.Fprintf(pc.Stdout(), "%-16s  %-20s  %-18s  %s\n", m.NodeId, m.Name, ipStr, status)
			}
		}
	}

	return connect.NewResponse(&pb.ListMembersResponse{Members: members}), nil
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
