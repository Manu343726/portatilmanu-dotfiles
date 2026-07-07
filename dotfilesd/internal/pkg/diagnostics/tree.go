package diagnostics

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"time"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

// FilterResources returns a flat list of resources matching the given filters.
func FilterResources(state *StateCache, filters ...FilterFunc) []*ResourceState {
	snapshot := state.Snapshot()
	keep := filterSnapshot(snapshot, 0, filters)
	result := make([]*ResourceState, 0, len(keep))
	for _, rs := range keep {
		result = append(result, rs)
	}
	return result
}

// ReconstructTree builds a DiagNode tree from the state cache.
//
// The algorithm proceeds in five phases:
//  1. Snapshot — atomic read of the state cache
//  2. Filter  — apply time-window gating and filter predicates
//  3. Adjacency — build parent→children maps, promote orphans
//  4. DFS assembly — recursively build DiagNode tree
//  5. Wrap root — synthetic "dotfilesd runtime" root node
func ReconstructTree(state *StateCache, timeWindow time.Duration, filters ...FilterFunc) *dotfilesdv1.DiagNode {
	// Phase 1: Snapshot.
	snapshot := state.Snapshot()
	nodeCount := len(snapshot)

	// Phase 2: Filter.
	keep := filterSnapshot(snapshot, timeWindow, filters)

	// Phase 3: Build adjacency.
	childrenOf := make(map[string][]*ResourceState)
	var roots []*ResourceState

	for _, res := range keep {
		parentID := res.ParentID
		if parentID == "" {
			roots = append(roots, res)
		} else if _, ok := keep[parentID]; ok {
			childrenOf[parentID] = append(childrenOf[parentID], res)
		} else {
			// Orphan — promote to root.
			roots = append(roots, res)
		}
	}

	// Sort by start time.
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].StartedAt.Before(roots[j].StartedAt)
	})
	for _, list := range childrenOf {
		sort.Slice(list, func(i, j int) bool {
			return list[i].StartedAt.Before(list[j].StartedAt)
		})
	}

	// Phase 4 & 5: DFS assembly and root wrap.
	root := &dotfilesdv1.DiagNode{
		Type:   dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_ROOT,
		Label:  "dotfilesd runtime",
		Status: dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_ACTIVE,
		Attrs: map[string]string{
			"node_count": fmt.Sprintf("%d", nodeCount),
		},
	}
	for _, res := range roots {
		root.Children = append(root.Children, buildNode(res, childrenOf))
	}
	return root
}

// filterSnapshot applies time-window gating and filter predicates to a snapshot.
func filterSnapshot(
	snapshot map[string]*ResourceState,
	timeWindow time.Duration,
	filters []FilterFunc,
) map[string]*ResourceState {
	now := time.Now()
	keep := make(map[string]*ResourceState, len(snapshot))

	for id, res := range snapshot {
		// Time-window gating.
		if res.Status != StatusActive {
			if timeWindow == 0 {
				continue // active-only default
			}
			if timeWindow > 0 && timeWindow != time.Duration(math.MaxInt64) {
				if res.FinishedAt != nil {
					age := now.Sub(*res.FinishedAt)
					if age > timeWindow {
						continue
					}
				}
			}
		}

		// Apply custom filter predicates.
		pass := true
		for _, fn := range filters {
			if !fn(res) {
				pass = false
				break
			}
		}
		if !pass {
			continue
		}

		keep[id] = res
	}
	return keep
}

// buildNode recursively assembles a DiagNode from a ResourceState and its children.
func buildNode(res *ResourceState, childrenOf map[string][]*ResourceState) *dotfilesdv1.DiagNode {
	node := &dotfilesdv1.DiagNode{
		Type:   diagNodeTypeFromString(res.Type),
		Label:  res.Label,
		Status: diagNodeStatusFromString(string(res.Status)),
	}
	node.Attrs = buildAttrs(res)

	for _, child := range childrenOf[res.ID] {
		node.Children = append(node.Children, buildNode(child, childrenOf))
	}
	return node
}

// buildAttrs constructs the attribute map for a DiagNode from a ResourceState.
//
// Every node includes:
//   - started, created, started_ago — always present
//   - running_for, running_for_ns — active only
//   - finished, finished_ago, finished_ago_ns, duration, duration_ns — finished only
//   - exit_code — when set
func buildAttrs(res *ResourceState) map[string]string {
	now := time.Now()
	attrs := make(map[string]string)

	if res.Attrs != nil {
		for k, v := range res.Attrs {
			attrs[k] = v
		}
	}

	// Always present.
	attrs["started"] = res.StartedAt.Format(time.RFC3339)
	attrs["created"] = res.CreatedAt.Format(time.RFC3339)
	attrs["started_ago"] = formatDuration(now.Sub(res.StartedAt))

	if res.IsActive() {
		attrs["running_for"] = formatDuration(now.Sub(res.StartedAt))
		attrs["running_for_ns"] = fmt.Sprintf("%d", now.Sub(res.StartedAt).Nanoseconds())
	} else if res.FinishedAt != nil {
		attrs["finished"] = res.FinishedAt.Format(time.RFC3339)
		attrs["finished_ago"] = formatDuration(now.Sub(*res.FinishedAt))
		attrs["finished_ago_ns"] = fmt.Sprintf("%d", now.Sub(*res.FinishedAt).Nanoseconds())
		if res.Duration != nil {
			attrs["duration"] = formatDuration(*res.Duration)
			attrs["duration_ns"] = fmt.Sprintf("%d", res.Duration.Nanoseconds())
		}
	}

	if res.ExitCode != nil {
		attrs["exit_code"] = fmt.Sprintf("%d", *res.ExitCode)
	}

	return attrs
}

// formatDuration returns a human-readable duration string.
func diagNodeTypeFromString(s string) dotfilesdv1.DiagNodeType {
	switch s {
	case "root":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_ROOT
	case "daemon":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_DAEMON
	case "client":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_CLIENT
	case "executor":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_EXECUTOR
	case "session":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_SESSION
	case "plugin":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_PLUGIN
	case "bg_task":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_BG_TASK
	case "shell":
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_SHELL
	default:
		return dotfilesdv1.DiagNodeType_DIAG_NODE_TYPE_UNSPECIFIED
	}
}

func diagNodeStatusFromString(s string) dotfilesdv1.DiagNodeStatus {
	switch s {
	case "active":
		return dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_ACTIVE
	case "pending":
		return dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_PENDING
	case "finished":
		return dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_FINISHED
	case "crashed":
		return dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_CRASHED
	default:
		return dotfilesdv1.DiagNodeStatus_DIAG_NODE_STATUS_UNSPECIFIED
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// Filter helpers — returned closures usable as FilterFunc.

// TypeFilter returns a filter that matches resources of the given types.
func TypeFilter(types ...string) FilterFunc {
	return func(rs *ResourceState) bool {
		for _, t := range types {
			if rs.Type == t {
				return true
			}
		}
		return len(types) == 0
	}
}

// StatusFilter returns a filter that matches resources with the given status.
func StatusFilter(status string) FilterFunc {
	return func(rs *ResourceState) bool {
		return string(rs.Status) == status
	}
}

// LabelFilter returns a filter that matches resources whose label matches the regex.
func LabelFilter(pattern string) FilterFunc {
	re := regexp.MustCompile(pattern)
	return func(rs *ResourceState) bool {
		return re.MatchString(rs.Label)
	}
}

// AttrFilter returns a filter that matches resources with the given attribute key=value.
func AttrFilter(key, value string) FilterFunc {
	return func(rs *ResourceState) bool {
		if rs.Attrs == nil {
			return false
		}
		return rs.Attrs[key] == value
	}
}
