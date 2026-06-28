package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
		Name:        info.Name,
		DisplayName: info.DisplayName,
		Version:     info.Version,
		Description: info.Description,
		Url:         info.URL,
		Services:    info.Services,
		Schemas:     info.Schemas,
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
			Name:        info.Name,
			DisplayName: info.DisplayName,
			Version:     info.Version,
			Description: info.Description,
			Url:         info.URL,
			Services:    info.Services,
			Schemas:     info.Schemas,
		})
	}

	resp := connect.NewResponse(&dotfilesdv1.RegistryListPluginsResponse{
		Plugins: plugins,
	})
	slog.Log(ctx, levelTrace, "Registry.ListPlugins done", "count", len(plugins))
	return resp, nil
}

func (s *registryServer) LoadPlugin(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.RegistryLoadPluginRequest],
) (*connect.Response[dotfilesdv1.RegistryLoadPluginResponse], error) {
	pluginName := req.Msg.PluginName
	if pluginName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("plugin_name is required"))
	}

	if s.daemon.pluginMgr == nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("plugin manager not available"))
	}

	info, loadedDeps, err := s.daemon.pluginMgr.LoadPluginByName(ctx, pluginName)
	if err != nil {
		return connect.NewResponse(&dotfilesdv1.RegistryLoadPluginResponse{
			Error: err.Error(),
		}), nil
	}

	return connect.NewResponse(&dotfilesdv1.RegistryLoadPluginResponse{
		Plugin: &dotfilesdv1.RegistryGetPluginResponse{
			Name:        info.Name,
			DisplayName: info.DisplayName,
			Version:     info.Version,
			Description: info.Description,
			Url:         info.URL,
			Services:    info.Services,
			Schemas:     info.Schemas,
		},
		LoadedDeps: loadedDeps,
	}), nil
}

func (s *registryServer) UnloadPlugin(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.RegistryUnloadPluginRequest],
) (*connect.Response[dotfilesdv1.RegistryUnloadPluginResponse], error) {
	pluginName := req.Msg.PluginName
	if pluginName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("plugin_name is required"))
	}

	if s.daemon.pluginMgr == nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("plugin manager not available"))
	}

	if err := s.daemon.pluginMgr.UnloadPluginByName(pluginName); err != nil {
		return connect.NewResponse(&dotfilesdv1.RegistryUnloadPluginResponse{
			Error: err.Error(),
		}), nil
	}

	return connect.NewResponse(&dotfilesdv1.RegistryUnloadPluginResponse{}), nil
}

func (s *registryServer) ReloadPlugins(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.RegistryReloadPluginsRequest],
) (*connect.Response[dotfilesdv1.RegistryReloadPluginsResponse], error) {
	if s.daemon.pluginMgr == nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("plugin manager not available"))
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	loaded, unloaded, err := s.daemon.pluginMgr.ReloadPlugins(ctx)
	resp := &dotfilesdv1.RegistryReloadPluginsResponse{
		Loaded:   loaded,
		Unloaded: unloaded,
	}
	if err != nil {
		resp.Error = err.Error()
	}
	return connect.NewResponse(resp), nil
}
