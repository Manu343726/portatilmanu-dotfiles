// TmuxBar plugin — provides formatted tmux status bar widgets.
//
// Depends on the resources plugin for system data (RAM, CPU).
// Each widget is a separate RPC that can be called individually.
package main

import (
	"context"
	"fmt"
	"net/http"

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
	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
		return connect.NewResponse(&pb.RAMWidgetResponse{Text: "RAM N/A"}), nil
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
	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
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
	// CPU temperature requires reading from /sys/class/thermal. For now,
	// return N/A since we don't have a dedicated data source.
	return connect.NewResponse(&pb.CPUTempWidgetResponse{Text: "🌡 N/A"}), nil
}

func (s *tmuxBarServer) BatteryWidget(ctx context.Context, req *connect.Request[pb.BatteryWidgetRequest]) (*connect.Response[pb.BatteryWidgetResponse], error) {
	// Battery info requires reading from /sys/class/power_supply. For now,
	// return N/A.
	return connect.NewResponse(&pb.BatteryWidgetResponse{Text: "🔋 N/A"}), nil
}

func (s *tmuxBarServer) StatusBar(ctx context.Context, req *connect.Request[pb.StatusBarRequest]) (*connect.Response[pb.StatusBarResponse], error) {
	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
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
	// Discover resources plugin via daemon's registry.
	daemonURL := "http://127.0.0.1:9105"
	httpClient := &http.Client{}
	regClient := dotfilesdv1connect.NewPluginRegistryServiceClient(httpClient, daemonURL)
	regResp, err := regClient.GetPlugin(context.Background(), connect.NewRequest(&dotfilesdv1.RegistryGetPluginRequest{
		PluginName: "resources",
	}))
	if err != nil {
		return nil
	}

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
