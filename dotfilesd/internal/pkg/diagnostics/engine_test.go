package diagnostics

import (
	"testing"
	"time"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

func TestEngine_PushEvent_CreatesResource(t *testing.T) {
	eng := New()

	eng.PushEvent(Event{
		Type:      EventPluginSpawn,
		Resource:  "plugin:weather",
		Parent:    "",
		Timestamp: time.Now(),
		Message:   "weather v1.0.0",
		Attrs:     map[string]string{"pid": "1234"},
	})

	tree := eng.GetCurrentTree(0)
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if len(tree.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tree.Children))
	}

	pluginNode := tree.Children[0]
	if pluginNode.Type != "plugin" {
		t.Errorf("expected type 'plugin', got %q", pluginNode.Type)
	}
	if pluginNode.Label != "weather v1.0.0" {
		t.Errorf("expected label 'weather v1.0.0', got %q", pluginNode.Label)
	}
	if pluginNode.Status != "active" {
		t.Errorf("expected status 'active', got %q", pluginNode.Status)
	}
	if pluginNode.Attrs["pid"] != "1234" {
		t.Errorf("expected pid attr '1234', got %q", pluginNode.Attrs["pid"])
	}
	if pluginNode.Attrs["started"] == "" {
		t.Error("expected started timestamp attr")
	}
	if pluginNode.Attrs["running_for"] == "" {
		t.Error("expected running_for attr for active resource")
	}
}

func TestEngine_PushEvent_Lifecycle(t *testing.T) {
	eng := New()
	now := time.Now()

	// Spawn plugin.
	eng.PushEvent(Event{
		ID:        "evt1",
		Type:      EventPluginSpawn,
		Resource:  "plugin:weather",
		Timestamp: now,
		Message:   "weather v1.0.0",
	})

	// Stop plugin 2 seconds later.
	eng.PushEvent(Event{
		ID:        "evt2",
		Type:      EventPluginStop,
		Resource:  "plugin:weather",
		Timestamp: now.Add(2 * time.Second),
		Attrs:     map[string]string{"exit_code": "0"},
	})

	// Active-only query (timeWindow=0) — should be empty.
	tree := eng.GetCurrentTree(0)
	if len(tree.Children) != 0 {
		t.Errorf("expected 0 children (finished), got %d", len(tree.Children))
	}

	// Query with time window covering the finish — should include it.
	tree = eng.GetCurrentTree(10 * time.Second)
	if len(tree.Children) != 1 {
		t.Fatalf("expected 1 child in time window, got %d", len(tree.Children))
	}

	pluginNode := tree.Children[0]
	if pluginNode.Status != "finished" {
		t.Errorf("expected status 'finished', got %q", pluginNode.Status)
	}
	if pluginNode.Attrs["duration"] == "" {
		t.Error("expected duration attr for finished resource")
	}
	if pluginNode.Attrs["duration_ns"] != "2000000000" {
		t.Errorf("expected duration_ns '2000000000', got %q", pluginNode.Attrs["duration_ns"])
	}
	if pluginNode.Attrs["exit_code"] != "0" {
		t.Errorf("expected exit_code '0', got %q", pluginNode.Attrs["exit_code"])
	}
}

func TestEngine_PushEvent_ParentChild(t *testing.T) {
	eng := New()
	now := time.Now()

	// Create daemon.
	eng.PushEvent(Event{
		Type:      EventDaemonStart,
		Resource:  "daemon",
		Timestamp: now,
		Message:   "dotfilesd",
	})

	// Spawn plugin under daemon's parent scope.
	eng.PushEvent(Event{
		Type:      EventPluginSpawn,
		Resource:  "plugin:weather",
		Parent:    "daemon",
		Timestamp: now,
		Message:   "weather v1.0.0",
	})

	// Client session under plugin.
	eng.PushEvent(Event{
		Type:      EventSessionCreate,
		Resource:  "session:ses_01",
		Parent:    "plugin:weather",
		Timestamp: now,
	})

	// Executor under session.
	eng.PushEvent(Event{
		Type:      EventExecutorOpen,
		Resource:  "executor:call_17",
		Parent:    "session:ses_01",
		Timestamp: now,
	})

	tree := eng.GetCurrentTree(0)
	if len(tree.Children) != 1 {
		t.Fatalf("expected 1 root (daemon), got %d", len(tree.Children))
	}

	// Root should be daemon.
	daemonNode := tree.Children[0]
	if daemonNode.Type != "daemon" {
		t.Fatalf("expected root child type 'daemon', got %q", daemonNode.Type)
	}

	// Daemon should have plugin child.
	if len(daemonNode.Children) != 1 {
		t.Fatalf("expected daemon to have 1 child (plugin), got %d", len(daemonNode.Children))
	}
	pluginNode := daemonNode.Children[0]

	// Plugin should have session child.
	if len(pluginNode.Children) != 1 {
		t.Fatalf("expected plugin to have 1 child (session), got %d", len(pluginNode.Children))
	}
	if pluginNode.Children[0].Type != "session" {
		t.Errorf("expected child type 'session', got %q", pluginNode.Children[0].Type)
	}

	// Session should have executor child.
	if len(pluginNode.Children[0].Children) != 1 {
		t.Fatalf("expected session to have 1 child (executor), got %d", len(pluginNode.Children[0].Children))
	}
	if pluginNode.Children[0].Children[0].Type != "executor" {
		t.Errorf("expected child type 'executor', got %q", pluginNode.Children[0].Children[0].Type)
	}
}

func TestEngine_GetResources_FlatList(t *testing.T) {
	eng := New()
	now := time.Now()

	eng.PushEvent(Event{Type: EventPluginSpawn, Resource: "plugin:weather", Timestamp: now})
	eng.PushEvent(Event{Type: EventPluginSpawn, Resource: "plugin:resources", Timestamp: now})
	eng.PushEvent(Event{Type: EventSessionCreate, Resource: "session:ses_01", Timestamp: now})

	resources := eng.GetResources()
	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(resources))
	}

	// Filter by type.
	filtered := eng.GetResources(TypeFilter("plugin"))
	if len(filtered) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(filtered))
	}

	// Filter by status.
	filtered = eng.GetResources(StatusFilter("active"))
	if len(filtered) != 3 {
		t.Fatalf("expected 3 active, got %d", len(filtered))
	}
}

func TestEngine_PushMetrics(t *testing.T) {
	eng := New()
	now := time.Now()

	eng.PushMetric(MetricPoint{
		Name:      "exec_duration_ms",
		Value:     42.5,
		Timestamp: now,
		Labels:    map[string]string{"plugin": "weather"},
	})
	eng.PushMetric(MetricPoint{
		Name:      "exec_duration_ms",
		Value:     100.0,
		Timestamp: now.Add(time.Second),
		Labels:    map[string]string{"plugin": "weather"},
	})

	metrics := eng.GetMetrics(nil)
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metric points, got %d", len(metrics))
	}

	// Name filter.
	metrics = eng.GetMetrics(func(m MetricPoint) bool {
		return m.Name == "exec_duration_ms"
	})
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metric points after name filter, got %d", len(metrics))
	}
}

func TestEngine_GetHistory(t *testing.T) {
	eng := New()
	now := time.Now()

	eng.PushEvent(Event{ID: "1", Type: EventPluginSpawn, Resource: "plugin:weather", Timestamp: now})
	eng.PushEvent(Event{ID: "2", Type: EventPluginCrash, Resource: "plugin:weather", Timestamp: now.Add(time.Second)})
	eng.PushEvent(Event{ID: "3", Type: EventSessionCreate, Resource: "session:ses_01", Timestamp: now.Add(2 * time.Second)})

	events := eng.GetHistory(nil)
	if len(events) != 3 {
		t.Fatalf("expected 3 history events, got %d", len(events))
	}

	// Filter by type.
	events = eng.GetHistory(func(evt Event) bool {
		return evt.Type == EventPluginCrash
	})
	if len(events) != 1 {
		t.Fatalf("expected 1 crash event, got %d", len(events))
	}
}

func TestEngine_PushSnapshot(t *testing.T) {
	eng := New()

	snapshot := &dotfilesdv1.DiagNode{
		Type:   "daemon",
		Label:  "dotfilesd (pid 1234, port 9105, up 100s)",
		Status: "active",
		Attrs:  map[string]string{"version": "0.1.0", "pid": "1234"},
		Children: []*dotfilesdv1.DiagNode{
			{
				Type:   "plugin",
				Label:  "weather v1.0.0",
				Status: "bg_worker",
				Attrs:  map[string]string{"pid": "5678"},
				Children: []*dotfilesdv1.DiagNode{
					{
						Type: "session", Label: "ses_01", Status: "active",
					},
				},
			},
		},
	}
	eng.PushSnapshot(snapshot)

	tree := eng.GetCurrentTree(0)
	if len(tree.Children) == 0 {
		t.Fatal("expected children after snapshot")
	}

	// Should have daemon node.
	var daemonNode *dotfilesdv1.DiagNode
	for _, c := range tree.Children {
		if c.Type == "daemon" {
			daemonNode = c
			break
		}
	}
	if daemonNode == nil {
		t.Fatal("expected daemon node after snapshot")
	}
	if daemonNode.Attrs["version"] != "0.1.0" {
		t.Errorf("expected version '0.1.0', got %q", daemonNode.Attrs["version"])
	}
	if len(daemonNode.Children) != 1 {
		t.Fatalf("expected 1 child (plugin), got %d", len(daemonNode.Children))
	}
}

func TestEngine_Subscribe(t *testing.T) {
	eng := New()
	ch := make(chan Event, 10)
	unsub := eng.Subscribe(ch)
	defer unsub()

	eng.PushEvent(Event{
		ID:       "test1",
		Type:     EventPluginSpawn,
		Resource: "plugin:test",
	})

	select {
	case evt := <-ch:
		if evt.ID != "test1" {
			t.Errorf("expected ID 'test1', got %q", evt.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event on subscriber channel")
	}

	// Unsubscribe and verify no more events.
	unsub()
	eng.PushEvent(Event{
		ID:       "test2",
		Type:     EventPluginSpawn,
		Resource: "plugin:test",
	})

	select {
	case <-ch:
		t.Error("received event after unsubscribe")
	case <-time.After(100 * time.Millisecond):
		// Expected — no event after unsubscribe.
	}
}

func TestEngine_CrashTransition(t *testing.T) {
	eng := New()
	now := time.Now()

	eng.PushEvent(Event{
		Type:      EventPluginSpawn,
		Resource:  "plugin:weather",
		Timestamp: now,
	})
	eng.PushEvent(Event{
		Type:      EventPluginCrash,
		Resource:  "plugin:weather",
		Timestamp: now.Add(time.Second),
		Attrs:     map[string]string{"exit_code": "139"},
	})

	// Query with time window.
	tree := eng.GetCurrentTree(10 * time.Second)
	if len(tree.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tree.Children))
	}

	if tree.Children[0].Status != "crashed" {
		t.Errorf("expected status 'crashed', got %q", tree.Children[0].Status)
	}
	if tree.Children[0].Attrs["exit_code"] != "139" {
		t.Errorf("expected exit_code '139', got %q", tree.Children[0].Attrs["exit_code"])
	}
	if tree.Children[0].Attrs["duration"] == "" {
		t.Error("expected duration attr")
	}
}

func TestEngine_IdempotentEvents(t *testing.T) {
	eng := New()
	now := time.Now()

	// Push the same event twice.
	evt := Event{
		ID:        "evt1",
		Type:      EventPluginSpawn,
		Resource:  "plugin:weather",
		Timestamp: now,
	}
	eng.PushEvent(evt)
	eng.PushEvent(evt) // duplicate — should be no-op

	tree := eng.GetCurrentTree(0)
	if len(tree.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tree.Children))
	}
	// Verify only one resource entry exists.
	resources := eng.GetResources()
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
}

func TestFilterResources_TypeFilter(t *testing.T) {
	eng := New()
	now := time.Now()

	eng.PushEvent(Event{Type: EventPluginSpawn, Resource: "plugin:a", Timestamp: now})
	eng.PushEvent(Event{Type: EventPluginSpawn, Resource: "plugin:b", Timestamp: now})
	eng.PushEvent(Event{Type: EventSessionCreate, Resource: "session:s1", Timestamp: now})
	eng.PushEvent(Event{Type: EventClientConnect, Resource: "client:c1", Timestamp: now})

	filtered := eng.GetResources(TypeFilter("plugin"))
	if len(filtered) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(filtered))
	}

	filtered = eng.GetResources(TypeFilter("session"))
	if len(filtered) != 1 {
		t.Errorf("expected 1 session, got %d", len(filtered))
	}

	filtered = eng.GetResources(TypeFilter("plugin", "session"))
	if len(filtered) != 3 {
		t.Errorf("expected 3 (plugins+sessions), got %d", len(filtered))
	}
}

func TestBuildAttrs_Timing(t *testing.T) {
	now := time.Now()
	finished := now.Add(5 * time.Second)
	dur := 5 * time.Second

	// Active resource.
	active := &ResourceState{
		Status:    StatusActive,
		StartedAt: now,
		CreatedAt: now,
	}
	attrs := buildAttrs(active)
	if attrs["started"] == "" {
		t.Error("expected started attr")
	}
	if attrs["running_for"] == "" {
		t.Error("expected running_for attr")
	}
	if attrs["running_for_ns"] == "" {
		t.Error("expected running_for_ns attr")
	}
	if attrs["finished"] != "" {
		t.Error("finished should not be present for active")
	}
	if attrs["duration"] != "" {
		t.Error("duration should not be present for active")
	}

	// Finished resource.
	finishedRes := &ResourceState{
		Status:     StatusFinished,
		StartedAt:  now,
		CreatedAt:  now,
		FinishedAt: &finished,
		Duration:   &dur,
	}
	attrs = buildAttrs(finishedRes)
	if attrs["finished"] == "" {
		t.Error("expected finished attr")
	}
	if attrs["finished_ago"] == "" {
		t.Error("expected finished_ago attr")
	}
	if attrs["duration"] == "" {
		t.Error("expected duration attr")
	}
	if attrs["duration_ns"] != "5000000000" {
		t.Errorf("expected duration_ns '5000000000', got %q", attrs["duration_ns"])
	}
	if attrs["running_for"] != "" {
		t.Error("running_for should not be present for finished")
	}
}
