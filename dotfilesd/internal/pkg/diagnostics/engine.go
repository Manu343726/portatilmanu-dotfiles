// Package diagnostics implements the dotfilesd diagnostics engine.
//
// The engine collects events and metrics from daemon components, maintains a
// resource state cache with lifecycle tracking, and provides both tree-based
// and flat-list queries with configurable time-window filtering.
//
// The engine is 100% independent from daemon internals — it knows about
// Event, ResourceState, and DiagNode only. Daemon components push data via
// PostService RPC or direct Go calls.
package diagnostics

import (
	"fmt"
	"sync"
	"time"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

// EventType categorizes a diagnostic event.
type EventType string

const (
	EventDaemonStart    EventType = "daemon_start"
	EventDaemonStop     EventType = "daemon_stop"
	EventPluginSpawn    EventType = "plugin_spawn"
	EventPluginCrash    EventType = "plugin_crash"
	EventPluginStop     EventType = "plugin_stop"
	EventClientConnect  EventType = "client_connect"
	EventClientDisconn  EventType = "client_disconnect"
	EventExecStart      EventType = "exec_start"
	EventExecStop       EventType = "exec_stop"
	EventExecutorOpen   EventType = "executor_open"
	EventExecutorClose  EventType = "executor_close"
	EventSessionCreate  EventType = "session_create"
	EventSessionEnd     EventType = "session_end"
	EventBgTaskStart    EventType = "bg_task_start"
	EventBgTaskStop     EventType = "bg_task_stop"
	EventScriptStart    EventType = "script_start"
	EventScriptStop     EventType = "script_stop"
	EventPluginRpcOpen  EventType = "plugin_rpc_open"
	EventPluginRpcClose EventType = "plugin_rpc_close"
)

// Event is a timestamped diagnostic event with structured payload.
// The Resource and Parent fields form the parent/child links for tree reconstruction.
type Event struct {
	ID        string
	Timestamp time.Time
	Type      EventType
	Resource  string // e.g. "plugin:weather", "session:ses_xxx"
	Parent    string // parent resource ID, empty = root-level
	Labels    map[string]string
	Message   string
	Attrs     map[string]string
}

// MetricPoint is a timestamped metric value.
type MetricPoint struct {
	Timestamp time.Time
	Name      string
	Value     float64
	Labels    map[string]string
}

// RetentionPolicy defines how long events/metrics of a type are kept.
type RetentionPolicy struct {
	MaxCount int           // max events to keep (0 = unlimited)
	MaxAge   time.Duration // max age before eviction
}

// FilterFunc is a predicate used to filter ResourceState entries.
type FilterFunc func(*ResourceState) bool

// Engine is the central diagnostics store.
//
// It maintains a state cache of tracked resources (reconstructed from events),
// ring-buffer history logs, and metric data points. All public methods are
// goroutine-safe.
type Engine struct {
	mu    sync.RWMutex
	state *StateCache

	history   map[EventType][]Event
	metrics   map[string][]MetricPoint
	retention map[EventType]RetentionPolicy

	// Subscribers for live updates.
	subs   map[string]chan Event
	subsMu sync.RWMutex
}

// New creates a new diagnostics engine with default retention policies.
//
// Defaults:
//   - Start/stop events: max 1000, 1 hour TTL
//   - Crash events: max 500, 24 hour TTL
//   - All other events: max 200, 5 minute TTL
func New() *Engine {
	e := &Engine{
		state:     NewStateCache(),
		history:   make(map[EventType][]Event),
		metrics:   make(map[string][]MetricPoint),
		retention: make(map[EventType]RetentionPolicy),
		subs:      make(map[string]chan Event),
	}
	// Default retention policies.
	defaults := map[EventType]RetentionPolicy{
		EventDaemonStart:    {MaxCount: 100, MaxAge: 24 * time.Hour},
		EventDaemonStop:     {MaxCount: 100, MaxAge: 24 * time.Hour},
		EventPluginSpawn:    {MaxCount: 500, MaxAge: time.Hour},
		EventPluginCrash:    {MaxCount: 500, MaxAge: 24 * time.Hour},
		EventPluginStop:     {MaxCount: 500, MaxAge: time.Hour},
		EventClientConnect:  {MaxCount: 1000, MaxAge: time.Hour},
		EventClientDisconn:  {MaxCount: 1000, MaxAge: time.Hour},
		EventExecStart:      {MaxCount: 200, MaxAge: 5 * time.Minute},
		EventExecStop:       {MaxCount: 200, MaxAge: 5 * time.Minute},
		EventExecutorOpen:   {MaxCount: 200, MaxAge: 5 * time.Minute},
		EventExecutorClose:  {MaxCount: 200, MaxAge: 5 * time.Minute},
		EventSessionCreate:  {MaxCount: 500, MaxAge: time.Hour},
		EventSessionEnd:     {MaxCount: 500, MaxAge: time.Hour},
		EventBgTaskStart:    {MaxCount: 200, MaxAge: 5 * time.Minute},
		EventBgTaskStop:     {MaxCount: 200, MaxAge: 5 * time.Minute},
		EventScriptStart:    {MaxCount: 200, MaxAge: time.Hour},
		EventScriptStop:     {MaxCount: 200, MaxAge: time.Hour},
		EventPluginRpcOpen:  {MaxCount: 500, MaxAge: time.Hour},
		EventPluginRpcClose: {MaxCount: 500, MaxAge: time.Hour},
	}
	for typ, p := range defaults {
		e.retention[typ] = p
	}
	return e
}

// SetRetention configures the retention policy for a given event type.
func (e *Engine) SetRetention(typ EventType, policy RetentionPolicy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retention[typ] = policy
}

// PushEvent records an event in the engine.
//
// It updates the resource state cache (creating or transitioning the
// ResourceState for the event's resource ID) and appends the event to
// the history ring buffer for its type. Subscribers are notified.
func (e *Engine) PushEvent(evt Event) {
	e.mu.Lock()

	// Update resource state.
	e.state.applyEvent(evt)

	// Append to history ring buffer.
	typ := evt.Type
	e.history[typ] = append(e.history[typ], evt)
	if rp, ok := e.retention[typ]; ok && rp.MaxCount > 0 && len(e.history[typ]) > rp.MaxCount {
		e.history[typ] = e.history[typ][len(e.history[typ])-rp.MaxCount:]
	}
	e.mu.Unlock()

	// Notify subscribers outside the lock.
	e.notifySubscribers(evt)
}

// UpdateParent changes the parent of an existing resource in the state cache.
// This is used to reparent sessions when they are used in the context of
// an exec command or executor call, giving the tree the correct ownership
// chain (e.g. client → exec → session).
func (e *Engine) UpdateParent(resourceID, newParent string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rs, ok := e.state.resources[resourceID]
	if !ok {
		return
	}
	rs.ParentID = newParent
	e.state.version++
}

// PushMetric records a metric data point.
func (e *Engine) PushMetric(m MetricPoint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics[m.Name] = append(e.metrics[m.Name], m)
	// Apply retention: keep last 1000 points per metric name.
	const maxMetricPoints = 1000
	if len(e.metrics[m.Name]) > maxMetricPoints {
		e.metrics[m.Name] = e.metrics[m.Name][len(e.metrics[m.Name])-maxMetricPoints:]
	}
}

// PushSnapshot replaces the current state for a resource subtree.
// Used for bulk state initialization.
func (e *Engine) PushSnapshot(node *dotfilesdv1.DiagNode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.applySnapshot(node, "")
}

func (e *Engine) applySnapshot(node *dotfilesdv1.DiagNode, parentID string) {
	id := node.GetType() + ":" + node.GetLabel()
	if parentID == "" {
		id = node.GetType()
	}

	startedAt := time.Now()
	rs := &ResourceState{
		ID:        id,
		Type:      node.GetType(),
		Label:     node.GetLabel(),
		ParentID:  parentID,
		Status:    StatusActive,
		CreatedAt: startedAt,
		StartedAt: startedAt,
		Attrs:     node.GetAttrs(),
	}
	e.state.resources[id] = rs
	e.state.version++

	for _, child := range node.GetChildren() {
		e.applySnapshot(child, id)
	}
}

// GetCurrentTree reconstructs the full state tree with the given time window and filters.
func (e *Engine) GetCurrentTree(timeWindow time.Duration, filters ...FilterFunc) *dotfilesdv1.DiagNode {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return ReconstructTree(e.state, timeWindow, filters...)
}

// GetResources returns a flat filtered list of resource states.
func (e *Engine) GetResources(filters ...FilterFunc) []*ResourceState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return FilterResources(e.state, filters...)
}

// GetHistory returns matching historical events.
func (e *Engine) GetHistory(filter func(Event) bool) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []Event
	for _, events := range e.history {
		for _, evt := range events {
			if filter == nil || filter(evt) {
				out = append(out, evt)
			}
		}
	}
	return out
}

// GetMetrics returns matching metric points.
func (e *Engine) GetMetrics(filter func(MetricPoint) bool) []MetricPoint {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []MetricPoint
	for _, points := range e.metrics {
		for _, m := range points {
			if filter == nil || filter(m) {
				out = append(out, m)
			}
		}
	}
	return out
}

// Subscribe registers a channel to receive all future events.
// The caller must read from the channel to avoid blocking.
// Returns an unsubscribe function.
func (e *Engine) Subscribe(ch chan Event) func() {
	id := fmt.Sprintf("%p", ch)
	e.subsMu.Lock()
	e.subs[id] = ch
	e.subsMu.Unlock()
	return func() {
		e.subsMu.Lock()
		delete(e.subs, id)
		e.subsMu.Unlock()
	}
}

func (e *Engine) notifySubscribers(evt Event) {
	e.subsMu.RLock()
	defer e.subsMu.RUnlock()
	for _, ch := range e.subs {
		select {
		case ch <- evt:
		default:
			// Drop if subscriber is not reading.
		}
	}
}
