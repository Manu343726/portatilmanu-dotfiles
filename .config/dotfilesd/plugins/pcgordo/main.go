package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	ha "github.com/mkelcik/go-ha-client"
	"connectrpc.com/connect"
	"dotfilesd/plugin"
	pb "plugins/pcgordo/proto/pcgordo"
	"plugins/pcgordo/proto/pcgordo/pcgordoconnect"
)

type pcgordoServer struct {
	client *ha.Client
}

func entityID(eid string) string       { return "button.pcgordo_" + eid }
func satelliteID(eid string) string     { return "button.pcgordo_satellite_" + eid }
func fullID(domain, name string) string { return domain + "." + name }

func parseDeviceClass(s string) pb.DeviceClass {
	switch s {
	case "temperature":
		return pb.DeviceClass_DEVICE_CLASS_TEMPERATURE
	case "power":
		return pb.DeviceClass_DEVICE_CLASS_POWER
	case "humidity":
		return pb.DeviceClass_DEVICE_CLASS_HUMIDITY
	case "pressure":
		return pb.DeviceClass_DEVICE_CLASS_PRESSURE
	case "battery":
		return pb.DeviceClass_DEVICE_CLASS_BATTERY
	case "energy":
		return pb.DeviceClass_DEVICE_CLASS_ENERGY
	case "current":
		return pb.DeviceClass_DEVICE_CLASS_CURRENT
	case "voltage":
		return pb.DeviceClass_DEVICE_CLASS_VOLTAGE
	case "speed":
		return pb.DeviceClass_DEVICE_CLASS_SPEED
	case "duration":
		return pb.DeviceClass_DEVICE_CLASS_DURATION
	case "monetary":
		return pb.DeviceClass_DEVICE_CLASS_MONETARY
	default:
		return pb.DeviceClass_DEVICE_CLASS_UNSPECIFIED
	}
}

// parsePCState reads an explicit pc_state entity (sensor/binary_sensor).
func parsePCState(s string) pb.PCState {
	switch s {
	case "on", "online", "true":
		return pb.PCState_PC_STATE_ONLINE
	default:
		return pb.PCState_PC_STATE_OFFLINE
	}
}

// zerotierPingEntity is published by Home Assistant itself (a ping binary
// sensor), not by the agent on PCGORDO.
const zerotierPingEntity = "binary_sensor.pcgordo_zerotier_ping"

// isAgentEntity reports whether an entity is published *by* the agent running
// on PCGORDO. The ones Home Assistant publishes itself describe the PC instead
// of reporting from it -- the ZeroTier ping binary_sensor is HA's own
// reachability probe (state "off" when the PC is down) and automations are HA's
// (state "on" unconditionally) -- so counting them as presence makes the PC look
// permanently online.
func isAgentEntity(entityID string) bool {
	if entityID == zerotierPingEntity {
		return false
	}
	return !strings.HasPrefix(entityID, "automation.")
}

// isConcreteState reports whether a state carries a real reading. HA marks every
// entity of a device unavailable (or unknown, for buttons) once the agent stops
// publishing, so an unavailable/unknown state means "the PC is not reporting".
func isConcreteState(s string) bool {
	switch s {
	case "", "unavailable", "unknown":
		return false
	default:
		return true
	}
}

func parseMonitorPowerState(s string) pb.MonitorPowerState {
	switch s {
	case "on":
		return pb.MonitorPowerState_MONITOR_POWER_STATE_ON
	case "off":
		return pb.MonitorPowerState_MONITOR_POWER_STATE_OFF
	default:
		return pb.MonitorPowerState_MONITOR_POWER_STATE_UNKNOWN
	}
}

func parseTimestamp(s string) int64 {
	if s == "" || s == "unknown" || s == "unavailable" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix()
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.Unix()
	}
	return 0
}

func (s *pcgordoServer) callButton(ctx context.Context, entityID string) (*connect.Response[pb.ActionResult], error) {
	_, err := s.client.CallService(ctx, ha.DefaultServiceCmd{
		Domain:   "button",
		Service:  "press",
		EntityId: entityID,
	})
	if err != nil {
		return connect.NewResponse(&pb.ActionResult{Success: false, Message: err.Error()}), nil
	}
	return connect.NewResponse(&pb.ActionResult{Success: true, Message: "ok"}), nil
}

func (s *pcgordoServer) Poweron(ctx context.Context, req *connect.Request[pb.PoweronRequest]) (*connect.Response[pb.PoweronResponse], error) {
	_, err := s.client.CallService(ctx, ha.DefaultServiceCmd{
		Domain:   "button",
		Service:  "press",
		EntityId: "button.wake_on_lan_bc_fc_e7_b2_e1_f5",
	})
	if err != nil {
		return connect.NewResponse(&pb.PoweronResponse{Success: false, Message: err.Error()}), nil
	}
	return connect.NewResponse(&pb.PoweronResponse{Success: true, Message: "WOL packet sent"}), nil
}

func (s *pcgordoServer) Shutdown(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.ActionResult], error) {
	return s.callButton(ctx, entityID("shutdown"))
}

func (s *pcgordoServer) Restart(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.ActionResult], error) {
	return s.callButton(ctx, entityID("restart"))
}

func (s *pcgordoServer) Hibernate(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.ActionResult], error) {
	return s.callButton(ctx, entityID("hibernate"))
}

func (s *pcgordoServer) MonitorSleep(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.ActionResult], error) {
	return s.callButton(ctx, entityID("monitorsleep"))
}

func (s *pcgordoServer) MonitorWake(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.ActionResult], error) {
	return s.callButton(ctx, entityID("monitorwake"))
}

func (s *pcgordoServer) SatelliteHibernate(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.ActionResult], error) {
	return s.callButton(ctx, satelliteID("hibernate"))
}

func (s *pcgordoServer) SatelliteRestart(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.ActionResult], error) {
	return s.callButton(ctx, satelliteID("restart"))
}

func (s *pcgordoServer) SatelliteShutdown(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.ActionResult], error) {
	return s.callButton(ctx, satelliteID("shutdown"))
}

func (s *pcgordoServer) Status(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.StatusResponse], error) {
	states, err := s.client.GetStates(ctx)
	if err != nil {
		return connect.NewResponse(&pb.StatusResponse{}), err
	}

	res := &pb.StatusResponse{}
	// An explicit pc_state entity, if one ever exists, is the only authority on
	// power state. Everything else is presence: see the fallback below.
	explicitPcState := false
	for _, st := range states {
		if !strings.HasPrefix(st.EntityId, "pcgordo") &&
			st.EntityId != zerotierPingEntity &&
			!strings.Contains(st.EntityId, "pcgordo") {
			continue
		}

		name, _ := st.Attributes["friendly_name"].(string)
		unit, _ := st.Attributes["unit_of_measurement"].(string)
		dc, _ := st.Attributes["device_class"].(string)

		es := &pb.EntityState{
			EntityId:     st.EntityId,
			FriendlyName: name,
			State:        st.State,
			Unit:         unit,
			DeviceClass:  parseDeviceClass(dc),
		}
		res.Entities = append(res.Entities, es)

		switch {
		case st.EntityId == zerotierPingEntity:
			res.ZerotierPingReachable = st.State == "on"
		case strings.HasSuffix(st.EntityId, "pc_state"):
			res.PcState = parsePCState(st.State)
			explicitPcState = true
		case strings.HasSuffix(st.EntityId, "_lastboot"):
			res.LastBoot = parseTimestamp(st.State)
		case strings.HasSuffix(st.EntityId, "_lastactive"):
			res.LastActive = parseTimestamp(st.State)
		case strings.HasSuffix(st.EntityId, "_cpuload"):
			fmt.Sscanf(st.State, "%f", &res.CpuLoad)
		case strings.HasSuffix(st.EntityId, "_gpuload"):
			fmt.Sscanf(st.State, "%f", &res.GpuLoad)
		case strings.HasSuffix(st.EntityId, "_gputemperature"):
			fmt.Sscanf(st.State, "%f", &res.GpuTemp)
		case strings.HasSuffix(st.EntityId, "_memoryusage"):
			fmt.Sscanf(st.State, "%f", &res.MemoryUsage)
		case strings.HasSuffix(st.EntityId, "_activewindow"):
			res.ActiveWindow = st.State
		case strings.HasSuffix(st.EntityId, "_activedesktop"):
			res.ActiveDesktop = st.State
		case strings.HasSuffix(st.EntityId, "_monitorpowerstate"):
			res.MonitorPower = parseMonitorPowerState(st.State)
		}

		// Presence fallback. Every entity reaching this point belongs to
		// PCGORDO, so any concrete state proves the PC is up and reporting --
		// whatever the entity is called. This matters because the only entity
		// that used to drive PcState was media_player.pcgordo_2, whose states
		// are playing/paused/idle/off and never the "on" that parsePCState
		// looks for, so a healthy PC always read as PC_STATE_OFFLINE. Matching
		// on a concrete state instead of a hardcoded entity id also survives
		// the _2 suffix changing when the HA device is re-registered.
		// This only ever promotes to ONLINE, so it can never contradict an
		// explicit pc_state that already said offline.
		if !explicitPcState && isAgentEntity(st.EntityId) && isConcreteState(st.State) {
			res.PcState = pb.PCState_PC_STATE_ONLINE
		}
	}
	if res.PcState == pb.PCState_PC_STATE_UNSPECIFIED {
		res.PcState = pb.PCState_PC_STATE_OFFLINE
	}

	if pc := plugin.ExtractContext(ctx); pc != nil && pc.RenderOutput() {
		w := pc.Stdout()
		fmt.Fprintf(w, "PC State:   %s\n", res.PcState)
		if res.LastBoot != 0 {
			fmt.Fprintf(w, "Last Boot:  %d\n", res.LastBoot)
		}
		if res.LastActive != 0 {
			fmt.Fprintf(w, "Last Active: %d\n", res.LastActive)
		}
		fmt.Fprintf(w, "CPU Load:   %.0f%%\n", res.CpuLoad)
		if res.GpuLoad > 0 {
			fmt.Fprintf(w, "GPU Load:   %.0f%%\n", res.GpuLoad)
		}
		if res.GpuTemp > 0 {
			fmt.Fprintf(w, "GPU Temp:   %.0f°C\n", res.GpuTemp)
		}
		fmt.Fprintf(w, "Memory:     %.0f%%\n", res.MemoryUsage)
		if res.ActiveWindow != "" {
			fmt.Fprintf(w, "Active Win: %s\n", res.ActiveWindow)
		}
		if res.MonitorPower != pb.MonitorPowerState_MONITOR_POWER_STATE_UNSPECIFIED {
			fmt.Fprintf(w, "Monitor:    %s\n", res.MonitorPower)
		}
		fmt.Fprintf(w, "ZeroTier:   %t\n", res.ZerotierPingReachable)
		fmt.Fprintf(w, "Entities:   %d\n", len(res.Entities))
	}

	return connect.NewResponse(res), nil
}

func (s *pcgordoServer) Screenshot(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.ScreenshotResponse], error) {
	ent, err := s.client.GetStateForEntity(ctx, "camera.pcgordo_screenshot")
	if err != nil {
		return connect.NewResponse(&pb.ScreenshotResponse{Available: false, Error: err.Error()}), nil
	}
	if ent.State == "unavailable" {
		return connect.NewResponse(&pb.ScreenshotResponse{Available: false, Error: "PC is offline"}), nil
	}
	url, _ := ent.Attributes["entity_picture"].(string)
	return connect.NewResponse(&pb.ScreenshotResponse{
		Available: true,
		ImageUrl:  url,
	}), nil
}

func (s *pcgordoServer) WindowsUpdates(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.WindowsUpdatesResponse], error) {
	states, err := s.client.GetStates(ctx)
	if err != nil {
		return connect.NewResponse(&pb.WindowsUpdatesResponse{}), err
	}

	res := &pb.WindowsUpdatesResponse{}
	for _, st := range states {
		if !strings.HasPrefix(st.EntityId, "sensor.pcgordo_windowsupdates") {
			continue
		}
		var v int
		fmt.Sscanf(st.State, "%d", &v)
		switch st.EntityId {
		case "sensor.pcgordo_windowsupdates_software_updates":
			res.AvailableSoftwareUpdates = int32(v)
		case "sensor.pcgordo_windowsupdates_software_updates_pending":
			res.PendingSoftwareUpdates = int32(v)
		case "sensor.pcgordo_windowsupdates_driver_updates":
			res.AvailableDriverUpdates = int32(v)
		case "sensor.pcgordo_windowsupdates_driver_updates_pending":
			res.PendingDriverUpdates = int32(v)
		}
	}
	return connect.NewResponse(res), nil
}

func getHAConfig() (host, token string) {
	host = os.Getenv("HA_MCP_URL")
	token = os.Getenv("HA_MCP_TOKEN")
	if host != "" && token != "" {
		return host, token
	}
	raw, _ := os.ReadFile(os.ExpandEnv("$HOME/.config/opencode/.env"))
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		k = strings.TrimPrefix(k, "export ")
		v = strings.Trim(v, "\"'")
		switch k {
		case "HA_MCP_URL":
			host = v
		case "HA_MCP_TOKEN":
			token = v
		}
	}
	return host, token
}

func main() {
	host, token := getHAConfig()
	if host == "" || token == "" {
		fmt.Fprintf(os.Stderr, "pcgordo: HA_MCP_URL and HA_MCP_TOKEN must be set\n")
		os.Exit(1)
	}

	// Extract base HA URL from MCP URL (strip /api/mcp suffix)
	haURL := strings.TrimSuffix(host, "/api/mcp")
	haURL = strings.TrimSuffix(haURL, "/")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	client := ha.NewClient(ha.ClientConfig{
		Token: token,
		Host:  haURL,
	}, httpClient)

	svc := &pcgordoServer{client: client}
	path, handler := pcgordoconnect.NewPcgordoServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "pcgordo",
		DisplayName: "PCGORDO",
		Version:     "1.0.0",
		Description: "Control and monitor the PCGORDO desktop PC (Salon) via Home Assistant",
		DocsProto: pb.PluginDocs,
		Services: []plugin.Service{
			{
				Name:             "pcgordo.PcgordoService",
				Description:      "PCGORDO power control, monitoring, and management",
				Path:             path,
				Handler:          handler,
				PluginAccessible: true,
			},
		},
	})
}
