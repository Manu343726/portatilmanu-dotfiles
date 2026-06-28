package diagnostics

import (
	"encoding/json"
	"sync"
	"time"
)

// ResourceStatus represents the lifecycle phase of a resource.
type ResourceStatus string

const (
	StatusPending  ResourceStatus = "pending"
	StatusActive   ResourceStatus = "active"
	StatusFinished ResourceStatus = "finished"
	StatusCrashed  ResourceStatus = "crashed"
)

// ResourceState tracks the full lifecycle of a single resource.
// Instances are created by the engine as events arrive and mutated in place
// under the StateCache lock.
type ResourceState struct {
	ID       string
	Type     string // "daemon", "plugin", "client", etc.
	Label    string // Human-readable name
	ParentID string // Empty = root-level (daemon child)

	// Lifecycle phase
	Status ResourceStatus

	// Timing
	CreatedAt time.Time // First event for this resource
	StartedAt time.Time // Start event timestamp

	// Timing — nil while resource is still active
	FinishedAt *time.Time
	Duration   *time.Duration

	// Metadata
	Attrs    map[string]string
	ExitCode *int
	Error    string
}

// IsActive returns true if the resource is currently active.
func (rs *ResourceState) IsActive() bool {
	return rs.Status == StatusActive
}

// StateCache is the engine's internal resource store with versioning.
type StateCache struct {
	mu        sync.RWMutex
	resources map[string]*ResourceState
	version   uint64
}

// NewStateCache creates a new empty state cache.
func NewStateCache() *StateCache {
	return &StateCache{
		resources: make(map[string]*ResourceState),
	}
}

// Snapshot returns a point-in-time copy of all resources.
func (sc *StateCache) Snapshot() map[string]*ResourceState {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	out := make(map[string]*ResourceState, len(sc.resources))
	for k, v := range sc.resources {
		cp := *v
		if v.Attrs != nil {
			cp.Attrs = make(map[string]string, len(v.Attrs))
			for ak, av := range v.Attrs {
				cp.Attrs[ak] = av
			}
		}
		out[k] = &cp
	}
	return out
}

// terminalStatus returns the appropriate ResourceStatus for an end event type.
func terminalStatus(typ EventType) ResourceStatus {
	switch typ {
	case EventPluginCrash:
		return StatusCrashed
	default:
		return StatusFinished
	}
}

// isStartType returns true if the event type represents a resource starting.
func isStartType(typ EventType) bool {
	switch typ {
	case EventDaemonStart, EventPluginSpawn, EventClientConnect,
		EventExecStart, EventExecutorOpen, EventSessionCreate,
		EventBgTaskStart, EventScriptStart:
		return true
	}
	return false
}

// isEndType returns true if the event type represents a resource ending.
func isEndType(typ EventType) bool {
	switch typ {
	case EventDaemonStop, EventPluginStop, EventPluginCrash,
		EventClientDisconn, EventExecStop, EventExecutorClose,
		EventSessionEnd, EventBgTaskStop, EventScriptStop:
		return true
	}
	return false
}

// applyEvent applies a diagnostic event to the state cache,
// creating or transitioning the resource as needed.
func (sc *StateCache) applyEvent(evt Event) {
	rs, exists := sc.resources[evt.Resource]
	if !exists {
		rs = &ResourceState{
			ID:        evt.Resource,
			Type:      resourceTypeFromID(evt.Resource),
			Label:     evt.Message,
			ParentID:  evt.Parent,
			Status:    StatusPending,
			CreatedAt: evt.Timestamp,
			Attrs:     make(map[string]string),
		}
		sc.resources[evt.Resource] = rs
	}

	if isStartType(evt.Type) && rs.StartedAt.IsZero() {
		rs.Status = StatusActive
		rs.StartedAt = evt.Timestamp
		if rs.CreatedAt.IsZero() {
			rs.CreatedAt = evt.Timestamp
		}
		// Merge attributes from event.
		for k, v := range evt.Attrs {
			rs.Attrs[k] = v
		}
		rs.Label = evt.Message
	}

	if isEndType(evt.Type) && rs.FinishedAt == nil {
		rs.Status = terminalStatus(evt.Type)
		now := evt.Timestamp
		rs.FinishedAt = &now
		dur := now.Sub(rs.StartedAt)
		rs.Duration = &dur
		for k, v := range evt.Attrs {
			rs.Attrs[k] = v
		}
		if codeStr, ok := evt.Attrs["exit_code"]; ok {
			code := 0
			// Parse error is ignored — will be 0.
			_ = json.Unmarshal([]byte(codeStr), &code)
			rs.ExitCode = &code
		}
	}

	// Merge event attributes even for info events.
	if !isStartType(evt.Type) && !isEndType(evt.Type) {
		for k, v := range evt.Attrs {
			rs.Attrs[k] = v
		}
	}

	sc.version++
}

// resourceTypeFromID extracts the resource type from a namespaced ID.
// e.g. "plugin:weather" → "plugin", "client:cli_abc" → "client"
func resourceTypeFromID(id string) string {
	for i := 0; i < len(id); i++ {
		if id[i] == ':' {
			return id[:i]
		}
	}
	return id
}
