package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dotfilesd/plugin"
	pb "plugins/weather/proto/weather"
	"plugins/weather/proto/weather/weatherconnect"

	"connectrpc.com/connect"
)

// weatherServer implements the type-safe WeatherService.
type weatherServer struct{}

// wttrInCurrent holds the relevant portion of wttr.in's JSON response.
type wttrInCurrent struct {
	TempC       string `json:"temp_C"`
	FeelsLikeC  string `json:"FeelsLikeC"`
	Humidity    string `json:"humidity"`
	WeatherDesc []struct {
		Value string `json:"value"`
	} `json:"weatherDesc"`
	Winddir16Point string `json:"winddir16Point"`
	WindspeedKmph  string `json:"windspeedKmph"`
	Visibility     string `json:"visibility"`
	Pressure       string `json:"pressure"`
	Cloudcover     string `json:"cloudcover"`
	PrecipMM       string `json:"precipMM"`
}

type wttrInRoot struct {
	CurrentCondition []wttrInCurrent `json:"current_condition"`
	NearestArea      []struct {
		AreaName []struct {
			Value string `json:"value"`
		} `json:"areaName"`
		Country []struct {
			Value string `json:"value"`
		} `json:"country"`
	} `json:"nearest_area"`
}

func (s *weatherServer) Forecast(
	ctx context.Context,
	req *connect.Request[pb.ForecastRequest],
) (*connect.Response[pb.ForecastResponse], error) {
	location := req.Msg.Location
	format := req.Msg.Format

	pc := plugin.ExtractContext(ctx)
	renderOutput := pc != nil && pc.RenderOutput()

	// Determine URL based on explicit format or RenderOutput flag.
	// When RenderOutput=false, default to JSON (structured data).
	// When RenderOutput=true, default to brief (human-readable).
	var url string
	switch {
	case format == "json":
		url = fmt.Sprintf("wttr.in/%s?format=j1", location)
	case format == "full":
		url = fmt.Sprintf("wttr.in/%s", location)
	case format != "":
		// Explicit format was given (e.g. "brief").
		url = fmt.Sprintf("wttr.in/%s?0", location)
	case !renderOutput:
		// No explicit format and caller wants raw data.
		url = fmt.Sprintf("wttr.in/%s?format=j1", location)
		format = "json"
	default:
		// No explicit format, default to brief human-readable.
		url = fmt.Sprintf("wttr.in/%s?0", location)
	}

	if pc != nil {
		pc.Log().Info("forecasting weather",
			"location", location,
			"format", format,
			"render_output", renderOutput,
			"url", url,
		)
	}

	cmd := fmt.Sprintf("curl -s --max-time 10 '%s'", url)

	if pc == nil {
		return connect.NewResponse(&pb.ForecastResponse{
			ErrorMessage: "no daemon context available",
			ExitCode:     -1,
		}), nil
	}

	result, err := pc.Exec(cmd)
	if err != nil {
		pc.Log().Error("weather: exec failed", "error", err, "cmd", cmd)
		if renderOutput {
			fmt.Fprintf(pc.Stderr(), "weather: exec failed: %v\n", err)
		}
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
		pc.Log().Error("weather: curl error", "exit_code", result.ExitCode, "stderr", result.Stderr)
		if renderOutput {
			fmt.Fprintf(pc.Stderr(), "weather: %s\n", errMsg)
		}
		return connect.NewResponse(&pb.ForecastResponse{
			ExitCode:     int32(result.ExitCode),
			ErrorMessage: errMsg,
		}), nil
	}

	// Always keep the raw structured output in the RPC response so
	// programmatic callers (plugin-to-plugin) get the data regardless
	// of the render flag.
	raw := strings.TrimSpace(result.Stdout)

	// When RenderOutput is true, write human-readable output to stdout
	// for the interactive user. The RPC response always contains the
	// original structured data so downstream callers can parse it.
	if renderOutput {
		var display string
		if format == "json" {
			if formatted := formatWeatherJSON(raw); formatted != "" {
				display = formatted
			}
		}
		if display == "" {
			display = raw
		}
		fmt.Fprintln(pc.Stdout(), display)
	}

	return connect.NewResponse(&pb.ForecastResponse{
		Report:   raw,
		ExitCode: 0,
	}), nil
}

// formatWeatherJSON parses wttr.in's JSON response (format=j1) and returns
// a human-readable formatted weather report. Returns empty string on error.
func formatWeatherJSON(raw string) string {
	var root wttrInRoot
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return ""
	}
	if len(root.CurrentCondition) == 0 {
		return ""
	}
	cc := root.CurrentCondition[0]

	// Build the report.
	var area, country string
	if len(root.NearestArea) > 0 {
		if len(root.NearestArea[0].AreaName) > 0 {
			area = root.NearestArea[0].AreaName[0].Value
		}
		if len(root.NearestArea[0].Country) > 0 {
			country = root.NearestArea[0].Country[0].Value
		}
	}

	var desc string
	if len(cc.WeatherDesc) > 0 {
		desc = cc.WeatherDesc[0].Value
	}

	var b strings.Builder
	if area != "" {
		fmt.Fprintf(&b, "📍 %s", area)
		if country != "" {
			fmt.Fprintf(&b, ", %s", country)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "🌡  %s°C", cc.TempC)
	if cc.FeelsLikeC != "" && cc.FeelsLikeC != cc.TempC {
		fmt.Fprintf(&b, " (feels like %s°C)", cc.FeelsLikeC)
	}
	b.WriteString("\n")
	if desc != "" {
		fmt.Fprintf(&b, "☁️  %s\n", desc)
	}
	if cc.Humidity != "" {
		fmt.Fprintf(&b, "💧 Humidity: %s%%\n", cc.Humidity)
	}
	if cc.WindspeedKmph != "" {
		fmt.Fprintf(&b, "💨 Wind: %s km/h %s\n", cc.WindspeedKmph, cc.Winddir16Point)
	}
	if cc.Pressure != "" {
		fmt.Fprintf(&b, "🔽 Pressure: %s mb\n", cc.Pressure)
	}
	if cc.Visibility != "" {
		fmt.Fprintf(&b, "👁  Visibility: %s km\n", cc.Visibility)
	}
	if cc.Cloudcover != "" {
		fmt.Fprintf(&b, "☁️  Cloud cover: %s%%\n", cc.Cloudcover)
	}
	if cc.PrecipMM != "" && cc.PrecipMM != "0.0" {
		fmt.Fprintf(&b, "🌧  Precipitation: %s mm\n", cc.PrecipMM)
	}
	return b.String()
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
