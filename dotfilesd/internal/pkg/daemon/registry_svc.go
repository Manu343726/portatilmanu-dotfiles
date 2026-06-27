package daemon

import (
	"context"
	"log/slog"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// registryServer implements PluginRegistryService.
type registryServer struct {
	sessions *SessionStore
	daemon   *Daemon
}

func newRegistryServer(sessions *SessionStore, daemon *Daemon) *registryServer {
	return &registryServer{sessions: sessions, daemon: daemon}
}

func (s *registryServer) GetPlugin(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.RegistryGetPluginRequest],
) (*connect.Response[dotfilesdv1.RegistryGetPluginResponse], error) {
	slog.Log(ctx, levelTrace, "Registry.GetPlugin", "plugin", req.Msg.PluginName)
	s.sessions.ResolveSession(req.Msg.GetSession())

	if s.daemon.pluginMgr == nil {
		return nil, connect.NewError(connect.CodeUnavailable, nil)
	}

	info, ok := s.daemon.pluginMgr.GetPlugin(req.Msg.PluginName)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}

	resp := connect.NewResponse(&dotfilesdv1.RegistryGetPluginResponse{
		Name:     info.Name,
		Url:      info.URL,
		Info:     info.Info,
		Services: info.Services,
	})
	slog.Log(ctx, levelTrace, "Registry.GetPlugin done", "plugin", req.Msg.PluginName, "url", info.URL)
	return resp, nil
}

func (s *registryServer) ListPlugins(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.RegistryListPluginsRequest],
) (*connect.Response[dotfilesdv1.RegistryListPluginsResponse], error) {
	slog.Log(ctx, levelTrace, "Registry.ListPlugins")
	s.sessions.ResolveSession(req.Msg.GetSession())

	if s.daemon.pluginMgr == nil {
		return connect.NewResponse(&dotfilesdv1.RegistryListPluginsResponse{}), nil
	}

	infos := s.daemon.pluginMgr.ListPlugins()
	plugins := make([]*dotfilesdv1.RegistryGetPluginResponse, 0, len(infos))
	for _, info := range infos {
		plugins = append(plugins, &dotfilesdv1.RegistryGetPluginResponse{
			Name:     info.Name,
			Url:      info.URL,
			Info:     info.Info,
			Services: info.Services,
		})
	}

	resp := connect.NewResponse(&dotfilesdv1.RegistryListPluginsResponse{
		Plugins: plugins,
	})
	slog.Log(ctx, levelTrace, "Registry.ListPlugins done", "count", len(plugins))
	return resp, nil
}
