package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"dotfilesd/internal/pkg/plugin"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// pluginServiceServer implements the PluginService usage-level RPCs.
// Both CLI tools and plugins use this service to discover and invoke
// plugin tools via the streaming CallPluginTool RPC.
type pluginServiceServer struct {
	sessions *SessionStore
	daemon   *Daemon
}

func newPluginServiceServer(sessions *SessionStore, daemon *Daemon) *pluginServiceServer {
	return &pluginServiceServer{sessions: sessions, daemon: daemon}
}

func (s *pluginServiceServer) ListPlugins(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ListPluginsRequest],
) (*connect.Response[dotfilesdv1.ListPluginsResponse], error) {
	slog.Log(ctx, levelTrace, "PluginService.ListPlugins", "request", req.Msg)
	s.sessions.ResolveSession(req.Msg.GetSession())

	if s.daemon.pluginMgr == nil {
		return connect.NewResponse(&dotfilesdv1.ListPluginsResponse{}), nil
	}

	plugins := s.daemon.pluginMgr.ListPlugins()
	protoPlugins := make([]*dotfilesdv1.ExtensionDescriptor, len(plugins))
	for i := range plugins {
		protoPlugins[i] = &plugins[i]
	}

	resp := connect.NewResponse(&dotfilesdv1.ListPluginsResponse{
		Plugins: protoPlugins,
	})
	slog.Log(ctx, levelTrace, "PluginService.ListPlugins done", "count", len(protoPlugins))
	return resp, nil
}

func (s *pluginServiceServer) ListPluginTree(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ListPluginTreeRequest],
) (*connect.Response[dotfilesdv1.ListPluginTreeResponse], error) {
	slog.Log(ctx, levelTrace, "PluginService.ListPluginTree", "request", req.Msg)
	s.sessions.ResolveSession(req.Msg.GetSession())

	if s.daemon.pluginMgr == nil {
		return connect.NewResponse(&dotfilesdv1.ListPluginTreeResponse{}), nil
	}

	tree := s.daemon.pluginMgr.ListPluginTree()
	protoEntries := make([]*dotfilesdv1.PluginTreeEntry, len(tree))
	for i := range tree {
		protoEntries[i] = plugin.ToProtoPluginTree(&tree[i])
	}

	resp := connect.NewResponse(&dotfilesdv1.ListPluginTreeResponse{
		Entries: protoEntries,
	})
	slog.Log(ctx, levelTrace, "PluginService.ListPluginTree done", "count", len(protoEntries))
	return resp, nil
}

func (s *pluginServiceServer) CallPluginTool(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.CallPluginToolRequest],
	stream *connect.ServerStream[dotfilesdv1.CallPluginToolResponse],
) error {
	slog.Log(ctx, levelTrace, "PluginService.CallPluginTool", "plugin", req.Msg.PluginName, "tool", req.Msg.ToolName)
	s.sessions.ResolveSession(req.Msg.GetSession())

	if s.daemon.pluginMgr == nil {
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("plugin system not initialized"))
	}

	// Open a streaming connection to the plugin's tool.
	pluginStream, err := s.daemon.pluginMgr.CallTool(ctx, req.Msg.PluginName, req.Msg.ToolName, req.Msg.Arguments)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("call plugin tool: %w", err))
	}

	// Relay chunks from the plugin stream to the caller stream.
	for pluginStream.Receive() {
		chunk := pluginStream.Msg()

		relay := &dotfilesdv1.CallPluginToolResponse{
			StdoutChunk: chunk.StdoutChunk,
			StderrChunk: chunk.StderrChunk,
		}
		if chunk.Done {
			relay.Done = true
			relay.ErrorMessage = chunk.ErrorMessage
			if err := stream.Send(relay); err != nil {
				return err
			}
			break
		}
		if err := stream.Send(relay); err != nil {
			return err
		}
	}

	if err := pluginStream.Err(); err != nil {
		slog.Log(ctx, levelTrace, "PluginService.CallPluginTool stream error", "plugin", req.Msg.PluginName, "tool", req.Msg.ToolName, "error", err)
		return err
	}

	slog.Log(ctx, levelTrace, "PluginService.CallPluginTool done", "plugin", req.Msg.PluginName, "tool", req.Msg.ToolName)
	return nil
}
