package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"dotfilesd/plugin"
	pb "plugins/display/proto/display"
	"plugins/display/proto/display/displayconnect"
)

type displayService struct{}

// outputStatusFromXrandr parses an xrandr output line and returns the status.
// xrandr lines look like:
//
//	eDP connected (normal ...)       -> CONNECTED
//	DP-0 connected (normal ...)      -> CONNECTED
//	DisplayPort-1-1 connected primary 2560x1440+0+0 ... -> CONNECTED (primary)
//	HDMI-A-0 disconnected (...)      -> DISCONNECTED
func (s *displayService) parseXrandrOutput(ctx context.Context) (string, string, []*pb.Output, error) {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return "", "", nil, fmt.Errorf("no plugin context")
	}

	result, err := pc.Exec("DISPLAY=:0 xrandr 2>/dev/null")
	if err != nil {
		return "", "", nil, fmt.Errorf("xrandr failed: %w", err)
	}

	internal := ""
	external := ""
	var outputs []*pb.Output

	lines := strings.Split(result.Stdout, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip resolution sub-lines and the "Screen 0:" header.
		// Only process lines where the second field is "connected" or "disconnected".
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		connState := fields[1]
		if connState != "connected" && connState != "disconnected" {
			continue
		}

		name := fields[0]

		status := pb.OutputStatus_OUTPUT_STATUS_DISCONNECTED
		if connState == "connected" {
			status = pb.OutputStatus_OUTPUT_STATUS_CONNECTED
		}

		primary := false
		resolution := ""

		// Parse rest of line for "primary" and resolution token.
		for _, token := range fields[2:] {
			if token == "primary" {
				primary = true
				continue
			}
			// Resolution is a token like "1920x1200" or "2560x1440+0+0".
			// Skip parenthesized tokens like "(normal" and standalone axis tokens.
			if resolution == "" && !strings.HasPrefix(token, "(") &&
				strings.Contains(token, "x") &&
				token[0] >= '0' && token[0] <= '9' {
				resolution = strings.Split(token, "+")[0]
			}
		}

		isInternal := strings.HasPrefix(name, "eDP") || name == "DP-0"

		outputs = append(outputs, &pb.Output{
			Name:       name,
			Status:     status,
			Primary:    primary,
			Resolution: resolution,
			Internal:   isInternal,
		})

		if status == pb.OutputStatus_OUTPUT_STATUS_CONNECTED && isInternal && internal == "" {
			internal = name
		}
		if status == pb.OutputStatus_OUTPUT_STATUS_CONNECTED && !isInternal && external == "" {
			external = name
		}
	}

	// Fallback: if we didn't find an eDP display but DP-0 is connected, it's internal
	if internal == "" {
		for _, o := range outputs {
			if o.Name == "DP-0" && o.Status == pb.OutputStatus_OUTPUT_STATUS_CONNECTED {
				internal = "DP-0"
				o.Internal = true
				break
			}
		}
	}

	return internal, external, outputs, nil
}

func (s *displayService) GetOutputs(ctx context.Context, req *connect.Request[pb.GetOutputsRequest]) (*connect.Response[pb.GetOutputsResponse], error) {
	pc := plugin.ExtractContext(ctx)

	internal, external, outputs, err := s.parseXrandrOutput(ctx)
	if err != nil {
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), "Error:", err)
		}
		return connect.NewResponse(&pb.GetOutputsResponse{}), nil
	}

	active := s.detectActiveLayout(outputs)

	resp := &pb.GetOutputsResponse{
		Outputs:      outputs,
		Internal:     internal,
		External:     external,
		ActiveLayout: active,
	}

	if pc != nil {
		pc.Log().Info("Display.GetOutputs", "internal", internal, "external", external, "layout", active.String())
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintf(pc.Stdout(), "Internal: %s\nExternal: %s\nLayout: %s\n", internal, external, active.String())
		for _, o := range outputs {
			fmt.Fprintf(pc.Stdout(), "  %s: %s primary=%v res=%s\n", o.Name, o.Status.String(), o.Primary, o.Resolution)
		}
	}

	return connect.NewResponse(resp), nil
}

func (s *displayService) detectActiveLayout(outputs []*pb.Output) pb.DisplayLayout {
	internalOn := false
	externalOn := false

	for _, o := range outputs {
		if o.Status != pb.OutputStatus_OUTPUT_STATUS_CONNECTED {
			continue
		}
		if o.Internal || o.Name == "DP-0" || strings.HasPrefix(o.Name, "eDP") {
			internalOn = true
		} else {
			externalOn = true
		}
	}

	if internalOn && !externalOn {
		return pb.DisplayLayout_DISPLAY_LAYOUT_LAPTOP_ONLY
	}
	if externalOn && !internalOn {
		return pb.DisplayLayout_DISPLAY_LAYOUT_EXTERNAL_ONLY
	}
	if internalOn && externalOn {
		return pb.DisplayLayout_DISPLAY_LAYOUT_EXTENDED
	}
	return pb.DisplayLayout_DISPLAY_LAYOUT_UNSPECIFIED
}

func (s *displayService) SetLayout(ctx context.Context, req *connect.Request[pb.SetLayoutRequest]) (*connect.Response[pb.SetLayoutResponse], error) {
	pc := plugin.ExtractContext(ctx)
	layout := req.Msg.Layout

	internal, external, _, err := s.parseXrandrOutput(ctx)
	if err != nil {
		msg := fmt.Sprintf("Error detecting displays: %v", err)
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), msg)
		}
		return connect.NewResponse(&pb.SetLayoutResponse{Success: false, Message: msg}), nil
	}

	if internal == "" {
		msg := "No internal display detected"
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), msg)
		}
		return connect.NewResponse(&pb.SetLayoutResponse{Success: false, Message: msg}), nil
	}

	var cmd string
	var msg string

	switch layout {
	case pb.DisplayLayout_DISPLAY_LAYOUT_LAPTOP_ONLY:
		if external != "" {
			cmd = fmt.Sprintf("xrandr --output %s --auto --primary --output %s --off", internal, external)
		} else {
			cmd = fmt.Sprintf("xrandr --output %s --auto --primary", internal)
		}
		msg = "Switched to laptop only"
	case pb.DisplayLayout_DISPLAY_LAYOUT_EXTERNAL_ONLY:
		if external == "" {
			msg = "No external display detected"
			if pc != nil && pc.RenderOutput() {
				fmt.Fprintln(pc.Stderr(), msg)
			}
			return connect.NewResponse(&pb.SetLayoutResponse{Success: false, Message: msg}), nil
		}
		cmd = fmt.Sprintf("xrandr --output %s --auto --primary --output %s --off", external, internal)
		msg = "Switched to external only"
	case pb.DisplayLayout_DISPLAY_LAYOUT_EXTENDED:
		if external == "" {
			msg = "No external display detected"
			if pc != nil && pc.RenderOutput() {
				fmt.Fprintln(pc.Stderr(), msg)
			}
			return connect.NewResponse(&pb.SetLayoutResponse{Success: false, Message: msg}), nil
		}
		cmd = fmt.Sprintf("xrandr --output %s --auto --primary --output %s --auto --right-of %s", internal, external, internal)
		msg = "Switched to extended layout"
	case pb.DisplayLayout_DISPLAY_LAYOUT_MIRROR:
		if external == "" {
			msg = "No external display detected"
			if pc != nil && pc.RenderOutput() {
				fmt.Fprintln(pc.Stderr(), msg)
			}
			return connect.NewResponse(&pb.SetLayoutResponse{Success: false, Message: msg}), nil
		}
		cmd = fmt.Sprintf("xrandr --output %s --auto --primary --output %s --auto --same-as %s", internal, external, internal)
		msg = "Switched to mirrored layout"
	default:
		msg = "Unknown layout"
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), msg)
		}
		return connect.NewResponse(&pb.SetLayoutResponse{Success: false, Message: msg}), nil
	}

	if pc != nil {
		pc.Log().Info("Display.SetLayout", "layout", layout.String(), "cmd", cmd)
	}

	_, err = pc.Exec(cmd)
	if err != nil {
		errMsg := fmt.Sprintf("xrandr failed: %v", err)
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), errMsg)
		}
		return connect.NewResponse(&pb.SetLayoutResponse{Success: false, Message: errMsg}), nil
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), msg)
	}

	return connect.NewResponse(&pb.SetLayoutResponse{Success: true, Message: msg}), nil
}

func (s *displayService) AutoExternal(ctx context.Context, req *connect.Request[pb.AutoExternalRequest]) (*connect.Response[pb.AutoExternalResponse], error) {
	pc := plugin.ExtractContext(ctx)

	settle := int(req.Msg.SettleSeconds)
	if settle <= 0 {
		settle = 2
	}

	if pc != nil {
		pc.Log().Info("Display.AutoExternal", "settle", settle)
	}

	time.Sleep(time.Duration(settle) * time.Second)

	_, external, _, err := s.parseXrandrOutput(ctx)
	if err != nil {
		msg := fmt.Sprintf("Error detecting displays: %v", err)
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), msg)
		}
		return connect.NewResponse(&pb.AutoExternalResponse{Switched: false, Message: msg}), nil
	}

	if external == "" {
		msg := "No external display detected"
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stdout(), msg)
		}
		return connect.NewResponse(&pb.AutoExternalResponse{Switched: false, Message: msg}), nil
	}

	// Apply external-only layout
	internal, _, _, err := s.parseXrandrOutput(ctx)
	if err != nil || internal == "" {
		// Fallback: just enable the external, leave internal as-is
		_, err = pc.Exec(fmt.Sprintf("xrandr --output %s --auto --primary", external))
	} else {
		_, err = pc.Exec(fmt.Sprintf("xrandr --output %s --auto --primary --output %s --off", external, internal))
	}

	if err != nil {
		msg := fmt.Sprintf("Failed to switch to external: %v", err)
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), msg)
		}
		return connect.NewResponse(&pb.AutoExternalResponse{Switched: false, External: external, Message: msg}), nil
	}

	msg := fmt.Sprintf("Auto-switched to external only: %s", external)
	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), msg)
	}

	return connect.NewResponse(&pb.AutoExternalResponse{Switched: true, External: external, Message: msg}), nil
}

func (s *displayService) AutorandrTrigger(ctx context.Context, req *connect.Request[pb.AutorandrTriggerRequest]) (*connect.Response[pb.AutorandrTriggerResponse], error) {
	pc := plugin.ExtractContext(ctx)

	settle := int(req.Msg.SettleSeconds)
	if settle <= 0 {
		settle = 1
	}

	if pc != nil {
		pc.Log().Info("Display.AutorandrTrigger", "settle", settle)
	}

	time.Sleep(time.Duration(settle) * time.Second)

	internal, external, _, err := s.parseXrandrOutput(ctx)
	if err != nil {
		msg := fmt.Sprintf("Error detecting displays: %v", err)
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), msg)
		}
		return connect.NewResponse(&pb.AutorandrTriggerResponse{
			ExternalConnected: false,
			LayoutApplied:     pb.DisplayLayout_DISPLAY_LAYOUT_UNSPECIFIED,
			Message:           msg,
		}), nil
	}

	if internal == "" {
		internal = "DP-0"
	}

	if external != "" {
		_, err = pc.Exec(fmt.Sprintf("xrandr --output %s --auto --primary --output %s --off", external, internal))
		if err != nil {
			msg := fmt.Sprintf("Failed to switch to external: %v", err)
			if pc != nil && pc.RenderOutput() {
				fmt.Fprintln(pc.Stderr(), msg)
			}
			return connect.NewResponse(&pb.AutorandrTriggerResponse{
				ExternalConnected: true,
				LayoutApplied:     pb.DisplayLayout_DISPLAY_LAYOUT_EXTERNAL_ONLY,
				Message:           msg,
			}), nil
		}
		msg := fmt.Sprintf("External plugged in: switched to %s only", external)
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stdout(), msg)
		}
		return connect.NewResponse(&pb.AutorandrTriggerResponse{
			ExternalConnected: true,
			LayoutApplied:     pb.DisplayLayout_DISPLAY_LAYOUT_EXTERNAL_ONLY,
			Message:           msg,
		}), nil
	}

	_, err = pc.Exec(fmt.Sprintf("xrandr --output %s --auto --primary", internal))
	if err != nil {
		msg := fmt.Sprintf("Failed to switch to internal: %v", err)
		if pc != nil && pc.RenderOutput() {
			fmt.Fprintln(pc.Stderr(), msg)
		}
		return connect.NewResponse(&pb.AutorandrTriggerResponse{
			ExternalConnected: false,
			LayoutApplied:     pb.DisplayLayout_DISPLAY_LAYOUT_LAPTOP_ONLY,
			Message:           msg,
		}), nil
	}

	msg := fmt.Sprintf("External unplugged: switched to %s only", internal)
	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), msg)
	}

	return connect.NewResponse(&pb.AutorandrTriggerResponse{
		ExternalConnected: false,
		LayoutApplied:     pb.DisplayLayout_DISPLAY_LAYOUT_LAPTOP_ONLY,
		Message:           msg,
	}), nil
}

func main() {
	svc := &displayService{}
	path, handler := displayconnect.NewDisplayServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "display",
		DisplayName: "Display",
		Version:     "1.0.0",
		Description: "Display output management via xrandr — detects internal/external displays and switches layouts",
		Services: []plugin.Service{
			{
				Name:        "display.DisplayService",
				Description: "Display output management API (xrandr wrapper)",
				Path:        path,
				Handler:     handler,
			},
		},
	})
}
