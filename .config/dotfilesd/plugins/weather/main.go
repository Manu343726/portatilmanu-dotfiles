package main

import (
	"context"
	"fmt"
	"strings"

	"dotfilesd/plugin"
	pb "plugins/weather/proto/weather"
	"plugins/weather/proto/weather/weatherconnect"

	"connectrpc.com/connect"
)

// weatherServer implements the type-safe WeatherService.
type weatherServer struct{}

func (s *weatherServer) Forecast(
	ctx context.Context,
	req *connect.Request[pb.ForecastRequest],
) (*connect.Response[pb.ForecastResponse], error) {
	location := req.Msg.Location
	format := req.Msg.Format

	pc := plugin.ExtractContext(ctx)

	var url string
	switch format {
	case "json":
		url = fmt.Sprintf("wttr.in/%s?format=j1", location)
	case "full":
		url = fmt.Sprintf("wttr.in/%s", location)
	default:
		url = fmt.Sprintf("wttr.in/%s?0", location)
	}

	if pc != nil {
		pc.Log().Info("forecasting weather via custom RPC", "location", location, "format", format, "url", url)
	}

	cmd := fmt.Sprintf("curl -s --max-time 10 '%s'", url)

	if pc != nil {
		result, err := pc.Exec(cmd)
		if err != nil {
			return connect.NewResponse(&pb.ForecastResponse{
				ErrorMessage: err.Error(),
				ExitCode:     -1,
			}), nil
		}
		if result.ExitCode != 0 {
			errMsg := strings.TrimSpace(result.Stderr)
			if errMsg == "" {
				errMsg = fmt.Sprintf("curl exited with code %d", result.ExitCode)
			}
			return connect.NewResponse(&pb.ForecastResponse{
				ExitCode:     int32(result.ExitCode),
				ErrorMessage: errMsg,
			}), nil
		}
		return connect.NewResponse(&pb.ForecastResponse{
			Report:   strings.TrimSpace(result.Stdout),
			ExitCode: 0,
		}), nil
	}

	return connect.NewResponse(&pb.ForecastResponse{
		ErrorMessage: "no daemon context available",
		ExitCode:     -1,
	}), nil
}

func main() {
	svc := &weatherServer{}
	path, handler := weatherconnect.NewWeatherServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "weather",
		DisplayName: "Weather",
		Version:     "1.0.0",
		Description: "Weather forecast plugin using wttr.in",
		Services: []plugin.Service{
			{
				Name:             "weather.WeatherService",
				Description:      "Type-safe weather forecast API for plugin-to-plugin calls",
				Path:             path,
				Handler:          handler,
				PluginAccessible: true,
			},
		},
	})
}
