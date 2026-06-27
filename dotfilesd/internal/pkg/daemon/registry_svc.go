package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// registryServer implements PluginRegistryService for plugin-to-plugin discovery.
type registryServer struct {
	sessions *SessionStore
	daemon   *Daemon
}

func newRegistryServer(sessions *SessionStore, daemon *Daemon) *registryServer {
	return &registryServer{sessions: sessions, daemon: daemon}
}

func (s *registryServer) GetPlugin(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.GetPluginRequest],
) (*connect.Response[dotfilesdv1.GetPluginResponse], error) {
	slog.Log(ctx, levelTrace, "Registry.GetPlugin", "plugin", req.Msg.PluginName)
	s.sessions.ResolveSession(req.Msg.GetSession())

	if s.daemon.pluginMgr == nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("plugin system not initialized"))
	}

	name := req.Msg.PluginName
	desc, ok := s.daemon.pluginMgr.GetDescriptor(name)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("plugin %q not found", name))
	}
	url, _ := s.daemon.pluginMgr.PluginURL(name)
	services, _ := s.daemon.pluginMgr.PluginServices(name)

	resp := connect.NewResponse(&dotfilesdv1.GetPluginResponse{
		Name:       name,
		Url:        url,
		Descriptor_: desc,
		Services:   services,
	})
	slog.Log(ctx, levelTrace, "Registry.GetPlugin done", "plugin", name, "url", url, "services", len(services))
	return resp, nil
}

func (s *registryServer) ListPlugins(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ListPluginsForRegistryRequest],
) (*connect.Response[dotfilesdv1.ListPluginsForRegistryResponse], error) {
	slog.Log(ctx, levelTrace, "Registry.ListPlugins")
	s.sessions.ResolveSession(req.Msg.GetSession())

	if s.daemon.pluginMgr == nil {
		return connect.NewResponse(&dotfilesdv1.ListPluginsForRegistryResponse{}), nil
	}

	infos := s.daemon.pluginMgr.ListPluginInfos()
	plugins := make([]*dotfilesdv1.GetPluginResponse, 0, len(infos))
	for _, info := range infos {
		url := ""
		if info.Process != nil {
			url = info.Process.URL
		}
		plugins = append(plugins, &dotfilesdv1.GetPluginResponse{
			Name:       info.Descriptor.Name,
			Url:        url,
			Descriptor_: info.Descriptor,
			Services:   info.Services,
		})
	}

	resp := connect.NewResponse(&dotfilesdv1.ListPluginsForRegistryResponse{
		Plugins: plugins,
	})
	slog.Log(ctx, levelTrace, "Registry.ListPlugins done", "count", len(plugins))
	return resp, nil
}
