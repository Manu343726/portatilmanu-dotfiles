package daemon

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"dotfilesd/internal/pkg/diagnostics"
	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
)

// diagnosticsPostServer implements DiagnosticsPostService.
// It receives events, metrics, and snapshots from daemon components and
// forwards them to the engine.
type diagnosticsPostServer struct {
	engine *diagnostics.Engine
}

func newDiagnosticsPostServer(engine *diagnostics.Engine) *diagnosticsPostServer {
	return &diagnosticsPostServer{engine: engine}
}

func (s *diagnosticsPostServer) PostEvent(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.DiagEvent],
) (*connect.Response[dotfilesdv1.PostEventResponse], error) {
	evt := req.Msg
	e := diagnostics.Event{
		ID:        evt.Id,
		Timestamp: time.Unix(0, evt.TimestampNs),
		Type:      diagnostics.EventType(evt.Type),
		Resource:  evt.Resource,
		Parent:    evt.Parent,
		Labels:    evt.Labels,
		Message:   evt.Message,
		Attrs:     evt.Attrs,
	}
	s.engine.PushEvent(e)
	return connect.NewResponse(&dotfilesdv1.PostEventResponse{}), nil
}

func (s *diagnosticsPostServer) PostMetric(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.MetricPoint],
) (*connect.Response[dotfilesdv1.PostMetricResponse], error) {
	m := req.Msg
	mp := diagnostics.MetricPoint{
		Timestamp: time.Unix(0, m.TimestampNs),
		Name:      m.Name,
		Value:     m.Value,
		Labels:    m.Labels,
	}
	s.engine.PushMetric(mp)
	return connect.NewResponse(&dotfilesdv1.PostMetricResponse{}), nil
}

func (s *diagnosticsPostServer) PostSnapshot(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.DiagNode],
) (*connect.Response[dotfilesdv1.PostSnapshotResponse], error) {
	s.engine.PushSnapshot(req.Msg)
	return connect.NewResponse(&dotfilesdv1.PostSnapshotResponse{}), nil
}

// diagnosticsQueryServer implements DiagnosticsQueryService.
// It serves tree, flat list, history, and metric queries from the engine.
type diagnosticsQueryServer struct {
	engine *diagnostics.Engine
}

func newDiagnosticsQueryServer(engine *diagnostics.Engine) *diagnosticsQueryServer {
	return &diagnosticsQueryServer{engine: engine}
}

func (s *diagnosticsQueryServer) QueryTree(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.QueryTreeRequest],
) (*connect.Response[dotfilesdv1.QueryTreeResponse], error) {
	r := req.Msg
	var filters []diagnostics.FilterFunc

	if len(r.IncludeTypes) > 0 {
		filters = append(filters, diagnostics.TypeFilter(r.IncludeTypes...))
	}
	if r.LabelRegex != "" {
		filters = append(filters, diagnostics.LabelFilter(r.LabelRegex))
	}
	if r.StatusFilter != "" {
		filters = append(filters, diagnostics.StatusFilter(r.StatusFilter))
	}
	for k, v := range r.AttrFilters {
		filters = append(filters, diagnostics.AttrFilter(k, v))
	}

	timeWindow := durationFromProto(r.TimeWindow)
	// Handle show_idle = true → no pruning.
	if r.ShowIdle && r.TimeWindow == nil {
		timeWindow = time.Duration(-1)
	}

	tree := s.engine.GetCurrentTree(timeWindow, filters...)
	return connect.NewResponse(&dotfilesdv1.QueryTreeResponse{Root: tree}), nil
}

func (s *diagnosticsQueryServer) QueryResources(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.QueryResourcesRequest],
) (*connect.Response[dotfilesdv1.QueryResourcesResponse], error) {
	r := req.Msg
	var filters []diagnostics.FilterFunc

	if len(r.IncludeTypes) > 0 {
		filters = append(filters, diagnostics.TypeFilter(r.IncludeTypes...))
	}
	if r.LabelRegex != "" {
		filters = append(filters, diagnostics.LabelFilter(r.LabelRegex))
	}
	if r.StatusFilter != "" {
		filters = append(filters, diagnostics.StatusFilter(r.StatusFilter))
	}
	for k, v := range r.AttrFilters {
		filters = append(filters, diagnostics.AttrFilter(k, v))
	}

	// For flat list we apply time window during filtering.
	// Pass the time window through the filters by wrapping.
	timeWindow := durationFromProto(r.TimeWindow)
	_ = timeWindow // The FilterResources function currently doesn't filter by time
	// TODO: Add time-window filtering to FilterResources in a follow-up.

	resources := s.engine.GetResources(filters...)

	// Sort.
	sortBy := r.SortBy
	sortDesc := r.SortDesc
	sort.Slice(resources, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "started_at":
			less = resources[i].StartedAt.Before(resources[j].StartedAt)
		case "type":
			less = resources[i].Type < resources[j].Type
		case "label":
			less = resources[i].Label < resources[j].Label
		case "status":
			less = resources[i].Status < resources[j].Status
		default:
			less = resources[i].StartedAt.Before(resources[j].StartedAt)
		}
		if sortDesc {
			return !less
		}
		return less
	})

	totalCount := len(resources)

	// Apply limit.
	limit := int(r.Limit)
	if limit > 0 && limit < len(resources) {
		resources = resources[:limit]
	}

	// Convert to proto.
	protoResources := make([]*dotfilesdv1.ResourceState, 0, len(resources))
	for _, rs := range resources {
		pr := &dotfilesdv1.ResourceState{
			Id:          rs.ID,
			Type:        rs.Type,
			Label:       rs.Label,
			ParentId:    rs.ParentID,
			Status:      string(rs.Status),
			CreatedAtNs: rs.CreatedAt.UnixNano(),
			StartedAtNs: rs.StartedAt.UnixNano(),
		}
		if rs.FinishedAt != nil {
			pr.FinishedAtNs = rs.FinishedAt.UnixNano()
		}
		if rs.Duration != nil {
			pr.DurationNs = rs.Duration.Nanoseconds()
		}
		if rs.Attrs != nil {
			pr.Attrs = rs.Attrs
		}
		if rs.ExitCode != nil {
			pr.ExitCode = int32(*rs.ExitCode)
		}
		protoResources = append(protoResources, pr)
	}

	return connect.NewResponse(&dotfilesdv1.QueryResourcesResponse{
		Resources:  protoResources,
		TotalCount: int32(totalCount),
	}), nil
}

func (s *diagnosticsQueryServer) QueryHistory(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.QueryHistoryRequest],
) (*connect.Response[dotfilesdv1.QueryHistoryResponse], error) {
	r := req.Msg
	since := time.Unix(0, r.SinceNs)
	until := time.Unix(0, r.UntilNs)
	limit := int(r.Limit)
	if limit <= 0 {
		limit = 100
	}

	typeSet := make(map[string]bool, len(r.Types))
	for _, t := range r.Types {
		typeSet[t] = true
	}

	events := s.engine.GetHistory(func(evt diagnostics.Event) bool {
		if len(typeSet) > 0 && !typeSet[string(evt.Type)] {
			return false
		}
		if r.SinceNs > 0 && evt.Timestamp.Before(since) {
			return false
		}
		if r.UntilNs > 0 && evt.Timestamp.After(until) {
			return false
		}
		if r.ResourceRegex != "" {
			if !strings.Contains(evt.Resource, r.ResourceRegex) {
				return false
			}
		}
		return true
	})

	// Sort by timestamp descending (newest first).
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})

	if len(events) > limit {
		events = events[:limit]
	}

	protoEvents := make([]*dotfilesdv1.DiagEvent, 0, len(events))
	for _, evt := range events {
		protoEvents = append(protoEvents, &dotfilesdv1.DiagEvent{
			Id:          evt.ID,
			TimestampNs: evt.Timestamp.UnixNano(),
			Type:        string(evt.Type),
			Resource:    evt.Resource,
			Parent:      evt.Parent,
			Labels:      evt.Labels,
			Message:     evt.Message,
			Attrs:       evt.Attrs,
		})
	}

	return connect.NewResponse(&dotfilesdv1.QueryHistoryResponse{
		Events: protoEvents,
	}), nil
}

func (s *diagnosticsQueryServer) QueryMetrics(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.QueryMetricsRequest],
) (*connect.Response[dotfilesdv1.QueryMetricsResponse], error) {
	r := req.Msg
	since := time.Unix(0, r.SinceNs)
	until := time.Unix(0, r.UntilNs)
	limit := int(r.Limit)
	if limit <= 0 {
		limit = 100
	}

	labelFilter := r.Labels

	points := s.engine.GetMetrics(func(m diagnostics.MetricPoint) bool {
		if r.NameRegex != "" && !strings.Contains(m.Name, r.NameRegex) {
			return false
		}
		if r.SinceNs > 0 && m.Timestamp.Before(since) {
			return false
		}
		if r.UntilNs > 0 && m.Timestamp.After(until) {
			return false
		}
		for k, v := range labelFilter {
			if m.Labels[k] != v {
				return false
			}
		}
		return true
	})

	sort.Slice(points, func(i, j int) bool {
		return points[i].Timestamp.After(points[j].Timestamp)
	})
	if len(points) > limit {
		points = points[:limit]
	}

	protoPoints := make([]*dotfilesdv1.MetricPoint, 0, len(points))
	for _, p := range points {
		protoPoints = append(protoPoints, &dotfilesdv1.MetricPoint{
			TimestampNs: p.Timestamp.UnixNano(),
			Name:        p.Name,
			Value:       p.Value,
			Labels:      p.Labels,
		})
	}

	return connect.NewResponse(&dotfilesdv1.QueryMetricsResponse{
		Points: protoPoints,
	}), nil
}

func (s *diagnosticsQueryServer) StreamEvents(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.StreamEventsRequest],
	stream *connect.ServerStream[dotfilesdv1.DiagEvent],
) error {
	r := req.Msg
	typeSet := make(map[string]bool, len(r.Types))
	for _, t := range r.Types {
		typeSet[t] = true
	}

	ch := make(chan diagnostics.Event, 100)
	cancel := s.engine.Subscribe(ch)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-ch:
			if !ok {
				return nil
			}
			if len(typeSet) > 0 && !typeSet[string(evt.Type)] {
				continue
			}
			protoEvt := &dotfilesdv1.DiagEvent{
				Id:          evt.ID,
				TimestampNs: evt.Timestamp.UnixNano(),
				Type:        string(evt.Type),
				Resource:    evt.Resource,
				Parent:      evt.Parent,
				Labels:      evt.Labels,
				Message:     evt.Message,
				Attrs:       evt.Attrs,
			}
			if err := stream.Send(protoEvt); err != nil {
				slog.Debug("stream events send error", "error", err)
				return err
			}
		}
	}
}

// durationFromProto converts a protobuf Duration to a Go time.Duration.
// Returns 0 if nil.
func durationFromProto(d *durationpb.Duration) time.Duration {
	if d == nil {
		return 0
	}
	return d.AsDuration()
}

var _ = durationpb.File_google_protobuf_duration_proto // import guard
