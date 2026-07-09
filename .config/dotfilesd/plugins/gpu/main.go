package main

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"dotfilesd/plugin"
	pb "plugins/gpu/proto/gpu"
	"plugins/gpu/proto/gpu/gpuconnect"
)

type gpuService struct{}

// runGpuctl runs supergfxctl and returns stdout.
// Falls back to sudo via pkexec if the direct call fails.
func (s *gpuService) runGpuctl(ctx context.Context, args ...string) (string, error) {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return "", fmt.Errorf("no plugin context")
	}

	cmd := "supergfxctl " + strings.Join(args, " ")
	result, err := pc.Exec(cmd)
	if err == nil {
		return strings.TrimSpace(result.Stdout), nil
	}

	// Retry with sudo as a fallback for systems that require it.
	result, err = pc.SudoExec(cmd)
	if err != nil {
		return "", fmt.Errorf("supergfxctl failed (tried sudo): %w", err)
	}
	return strings.TrimSpace(result.Stdout), nil
}

// readEgpu checks the ASUS WMI sysfs for eGPU connection status.
func (s *gpuService) readEgpu(ctx context.Context) bool {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return false
	}
	result, err := pc.Exec("cat /sys/devices/platform/asus-nb-wmi/egpu_connected 2>/dev/null")
	if err != nil {
		return false
	}
	return strings.TrimSpace(result.Stdout) == "1"
}

func parseGpuMode(s string) pb.GpuMode {
	switch s {
	case "Integrated":
		return pb.GpuMode_GPU_MODE_INTEGRATED
	case "Hybrid":
		return pb.GpuMode_GPU_MODE_HYBRID
	case "AsusMuxDgpu":
		return pb.GpuMode_GPU_MODE_ASUS_MUX_DGPU
	case "AsusEgpu":
		return pb.GpuMode_GPU_MODE_ASUS_EGPU
	default:
		return pb.GpuMode_GPU_MODE_UNSPECIFIED
	}
}

func modeDisplayName(m pb.GpuMode) string {
	switch m {
	case pb.GpuMode_GPU_MODE_INTEGRATED:
		return "Integrated"
	case pb.GpuMode_GPU_MODE_HYBRID:
		return "Hybrid"
	case pb.GpuMode_GPU_MODE_ASUS_MUX_DGPU:
		return "AsusMuxDgpu"
	case pb.GpuMode_GPU_MODE_ASUS_EGPU:
		return "AsusEgpu"
	default:
		return "Unknown"
	}
}

func (s *gpuService) GetProfile(ctx context.Context, req *connect.Request[pb.GetProfileRequest]) (*connect.Response[pb.GetProfileResponse], error) {
	pc := plugin.ExtractContext(ctx)

	currentStr, err := s.runGpuctl(ctx, "-g")
	if err != nil {
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), "Error:", err)
		}
		return connect.NewResponse(&pb.GetProfileResponse{}), nil
	}

	pendingStr, _ := s.runGpuctl(ctx, "-P")

	current := parseGpuMode(currentStr)
	if pc != nil {
		pc.Log().Info("Gpu.GetProfile", "current", currentStr, "pending", pendingStr)
	}

	resp := &pb.GetProfileResponse{
		Current:            current,
		CurrentDisplayName: currentStr,
		StatusMessage:      "",
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintf(pc.Stdout(), "Current: %s\n", currentStr)
		if pendingStr != "" && pendingStr != currentStr {
			fmt.Fprintf(pc.Stdout(), "Pending: %s\n", pendingStr)
		}
	}

	return connect.NewResponse(resp), nil
}

func (s *gpuService) SetProfile(ctx context.Context, req *connect.Request[pb.SetProfileRequest]) (*connect.Response[pb.SetProfileResponse], error) {
	pc := plugin.ExtractContext(ctx)
	mode := req.Msg.Mode

	modeStr := modeDisplayName(mode)
	if modeStr == "Unknown" {
		msg := fmt.Sprintf("Unknown GPU mode: %v", mode)
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), msg)
		}
		return connect.NewResponse(&pb.SetProfileResponse{Success: false, Message: msg}), nil
	}

	if pc != nil {
		pc.Log().Info("Gpu.SetProfile", "mode", modeStr)
	}

	// The supergfxctl -m output is on stdout for success, but the
	// "reboot required" messages go there too. We capture everything.
	out, err := s.runGpuctl(ctx, "-m", modeStr)
	if err != nil {
		msg := fmt.Sprintf("Failed to set GPU mode: %v", err)
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), msg)
		}
		return connect.NewResponse(&pb.SetProfileResponse{Success: false, Message: msg}), nil
	}

	successMsg := fmt.Sprintf("GPU mode set to %s", modeStr)
	if out != "" {
		successMsg = out
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), successMsg)
	}

	return connect.NewResponse(&pb.SetProfileResponse{Success: true, Message: successMsg}), nil
}

func (s *gpuService) ListProfiles(ctx context.Context, req *connect.Request[pb.ListProfilesRequest]) (*connect.Response[pb.ListProfilesResponse], error) {
	pc := plugin.ExtractContext(ctx)

	currentStr, err := s.runGpuctl(ctx, "-g")
	if err != nil {
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), "Error:", err)
		}
		return connect.NewResponse(&pb.ListProfilesResponse{}), nil
	}

	current := parseGpuMode(currentStr)
	egpu := s.readEgpu(ctx)

	allModes := []pb.GpuMode{
		pb.GpuMode_GPU_MODE_INTEGRATED,
		pb.GpuMode_GPU_MODE_HYBRID,
		pb.GpuMode_GPU_MODE_ASUS_MUX_DGPU,
		pb.GpuMode_GPU_MODE_ASUS_EGPU,
	}

	var profiles []*pb.GpuProfile
	for _, m := range allModes {
		status := pb.GpuModeStatus_GPU_MODE_STATUS_AVAILABLE
		dn := modeDisplayName(m)
		isCurrent := m == current

		// AsusEgpu is only available when eGPU hardware is connected.
		if m == pb.GpuMode_GPU_MODE_ASUS_EGPU {
			if !egpu {
				status = pb.GpuModeStatus_GPU_MODE_STATUS_UNAVAILABLE
			}
		}

		profiles = append(profiles, &pb.GpuProfile{
			Mode:          m,
			DisplayName:   dn,
			Status:        status,
			EgpuConnected: egpu && m == pb.GpuMode_GPU_MODE_ASUS_EGPU,
		})

		_ = isCurrent
	}

	if pc != nil {
		pc.Log().Info("Gpu.ListProfiles", "current", currentStr, "egpu", egpu, "count", len(profiles))
	}

	resp := &pb.ListProfilesResponse{
		Profiles:           profiles,
		Current:            current,
		CurrentDisplayName: currentStr,
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), "Current: "+currentStr)
		for _, p := range profiles {
			mark := " "
			if p.Mode == current {
				mark = "*"
			}
			line := "  " + mark + " " + p.DisplayName
			if p.Status == pb.GpuModeStatus_GPU_MODE_STATUS_UNAVAILABLE {
				line += " (unavailable)"
			}
			fmt.Fprintln(pc.Stdout(), line)
		}
	}

	return connect.NewResponse(resp), nil
}

func main() {
	svc := &gpuService{}
	path, handler := gpuconnect.NewGpuServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "gpu",
		DisplayName: "GPU",
		Version:     "1.0.0",
		Description: "GPU mode management via supergfxctl — switch between Integrated, Hybrid, AsusMuxDgpu, and AsusEgpu",
		Services: []plugin.Service{
			{
				Name:        "gpu.GpuService",
				Description: "GPU profile management API (supergfxctl wrapper)",
				Path:        path,
				Handler:     handler,
			},
		},
	})
}
