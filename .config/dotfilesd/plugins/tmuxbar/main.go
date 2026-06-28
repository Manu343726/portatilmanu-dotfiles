// TmuxBar plugin — provides formatted tmux status bar widgets.
//
// Depends on the resources plugin for system data (RAM, CPU).
// Each widget is a separate RPC that can be called individually.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"dotfilesd/plugin"
	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
	respb "plugins/resources/proto/resources"
	"plugins/resources/proto/resources/resourcesconnect"
	pb "plugins/tmuxbar/proto/tmuxbar"
	"plugins/tmuxbar/proto/tmuxbar/tmuxbarconnect"

	"connectrpc.com/connect"
)

// tmuxBarServer implements TmuxBarService by calling the resources plugin.
type tmuxBarServer struct {
	resourcesClient resourcesconnect.ResourcesServiceClient
}

func (s *tmuxBarServer) RAMWidget(ctx context.Context, req *connect.Request[pb.RAMWidgetRequest]) (*connect.Response[pb.RAMWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc != nil {
		peer := req.Peer()
		pc.Log().Info("▶ TmuxBar.RAMWidget",
			"peer", peer.Addr,
			"protocol", peer.Protocol,
			"render_output", pc.RenderOutput(),
		)
	}

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
		if pc != nil {
			pc.Log().Error("✗ TmuxBar.RAMWidget: resources.Current", "error", err)
		}
		return connect.NewResponse(&pb.RAMWidgetResponse{Text: "RAM N/A"}), nil
	}
	if pc != nil {
		pc.Log().Info("✓ TmuxBar.RAMWidget: resources.Current done", "ram_pct", r.Msg.Ram.GetPercent())
	}
	ram := r.Msg.Ram
	if ram == nil {
		return connect.NewResponse(&pb.RAMWidgetResponse{Text: "RAM N/A"}), nil
	}
	usedGB := ram.UsedMb / 1024
	totalGB := ram.TotalMb / 1024
	text := fmt.Sprintf("RAM %.1f/%.1f GB %.0f%%", usedGB, totalGB, ram.Percent)
	return connect.NewResponse(&pb.RAMWidgetResponse{Text: text, Percent: ram.Percent}), nil
}

func (s *tmuxBarServer) CPUWidget(ctx context.Context, req *connect.Request[pb.CPUWidgetRequest]) (*connect.Response[pb.CPUWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc != nil {
		peer := req.Peer()
		pc.Log().Info("▶ TmuxBar.CPUWidget",
			"peer", peer.Addr,
			"protocol", peer.Protocol,
			"render_output", pc.RenderOutput(),
		)
	}

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
		if pc != nil {
			pc.Log().Error("✗ TmuxBar.CPUWidget: resources.Current", "error", err)
		}
		return connect.NewResponse(&pb.CPUWidgetResponse{Text: "CPU N/A"}), nil
	}
	cpu := r.Msg.Cpu
	if cpu == nil {
		return connect.NewResponse(&pb.CPUWidgetResponse{Text: "CPU N/A"}), nil
	}
	text := fmt.Sprintf("CPU %.0f%%", cpu.TotalPercent)
	return connect.NewResponse(&pb.CPUWidgetResponse{Text: text, Percent: cpu.TotalPercent}), nil
}

func (s *tmuxBarServer) CPUTempWidget(ctx context.Context, req *connect.Request[pb.CPUTempWidgetRequest]) (*connect.Response[pb.CPUTempWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc != nil {
		peer := req.Peer()
		pc.Log().Info("▶ TmuxBar.CPUTempWidget",
			"peer", peer.Addr,
			"protocol", peer.Protocol,
			"render_output", pc.RenderOutput(),
		)
	}
	return connect.NewResponse(&pb.CPUTempWidgetResponse{Text: "🌡 N/A"}), nil
}

func (s *tmuxBarServer) BatteryWidget(ctx context.Context, req *connect.Request[pb.BatteryWidgetRequest]) (*connect.Response[pb.BatteryWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc != nil {
		peer := req.Peer()
		pc.Log().Info("▶ TmuxBar.BatteryWidget",
			"peer", peer.Addr,
			"protocol", peer.Protocol,
			"render_output", pc.RenderOutput(),
		)
	}
	return connect.NewResponse(&pb.BatteryWidgetResponse{Text: "🔋 N/A"}), nil
}

func (s *tmuxBarServer) StatusBar(ctx context.Context, req *connect.Request[pb.StatusBarRequest]) (*connect.Response[pb.StatusBarResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc != nil {
		peer := req.Peer()
		pc.Log().Info("▶ TmuxBar.StatusBar",
			"peer", peer.Addr,
			"protocol", peer.Protocol,
			"render_output", pc.RenderOutput(),
		)
	}

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
		if pc != nil {
			pc.Log().Error("✗ TmuxBar.StatusBar: resources.Current", "error", err)
		}
		return connect.NewResponse(&pb.StatusBarResponse{Text: "resources unavailable"}), nil
	}

	parts := ""
	if r.Msg.Ram != nil {
		parts += fmt.Sprintf("RAM %.0f%%", r.Msg.Ram.Percent)
	}
	if r.Msg.Cpu != nil {
		if parts != "" {
			parts += " | "
		}
		parts += fmt.Sprintf("CPU %.0f%%", r.Msg.Cpu.TotalPercent)
	}

	return connect.NewResponse(&pb.StatusBarResponse{Text: parts}), nil
}

func initResourcesClient() resourcesconnect.ResourcesServiceClient {
	daemonURL := "http://127.0.0.1:9105"
	httpClient := &http.Client{}
	regClient := dotfilesdv1connect.NewPluginRegistryServiceClient(httpClient, daemonURL)
	regResp, err := regClient.GetPlugin(context.Background(), connect.NewRequest(&dotfilesdv1.RegistryGetPluginRequest{
		PluginName: "resources",
	}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "tmuxbar: initResourcesClient: GetPlugin failed: %v\n", err)
		return nil
	}
	fmt.Fprintf(os.Stderr, "tmuxbar: initResourcesClient: resources at %s\n", regResp.Msg.Url)
	return resourcesconnect.NewResourcesServiceClient(httpClient, regResp.Msg.Url)
}

func main() {
	resClient := initResourcesClient()
	svc := &tmuxBarServer{resourcesClient: resClient}
	path, handler := tmuxbarconnect.NewTmuxBarServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "tmuxbar",
		DisplayName: "TmuxBar",
		Version:     "1.0.0",
		Description: "Tmux status bar widgets using resources plugin",
		Services: []plugin.Service{
			{
				Name:             "tmuxbar.TmuxBarService",
				Description:      "Tmux status bar widget API",
				Path:             path,
				Handler:          handler,
				PluginAccessible: true,
			},
		},
	})
}
