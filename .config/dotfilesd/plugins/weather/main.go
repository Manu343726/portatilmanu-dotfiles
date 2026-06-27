package main

import (
	"fmt"
	"strings"

	"dotfilesd/plugin"
	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

func main() {
	plugin.Serve(plugin.Config{
		Name:        "weather",
		DisplayName: "Weather",
		Version:     "1.0.0",
		Description: "Weather forecast plugin using wttr.in",
		Tools: []plugin.Tool{
			plugin.NewTool("forecast", "Get weather forecast for a location",
				&dotfilesdv1.ToolInputSchema{
					Properties: map[string]*dotfilesdv1.PropertySchema{
						"location": {
							Type:        dotfilesdv1.PropertyType_PROPERTY_TYPE_STRING,
							Description: "Location to get weather for (city name, ZIP code, IP address, etc.)",
						},
						"format": {
							Type:        dotfilesdv1.PropertyType_PROPERTY_TYPE_STRING,
							Description: "Output format (brief, full, json)",
							Default:     "brief",
						},
					},
					Required: []string{"location"},
				},
				&dotfilesdv1.CLIHints{
					CommandPath: "weather forecast",
					Category:    "utilities",
				},
				forecastFn,
			),
		},
	})
}

func forecastFn(ctx plugin.Context, args map[string]string) error {
	location := args["location"]
	if location == "" {
		return fmt.Errorf("location is required")
	}

	format := args["format"]
	if format == "" {
		format = "brief"
	}

	ctx.Log().Info("forecasting weather", "location", location, "format", format)

	var url string
	switch format {
	case "json":
		url = fmt.Sprintf("wttr.in/%s?format=j1", location)
	case "full":
		url = fmt.Sprintf("wttr.in/%s", location)
	default:
		url = fmt.Sprintf("wttr.in/%s?0", location)
	}

	ctx.Log().Debug("fetching weather", "url", url)

	result, err := ctx.Exec(fmt.Sprintf("curl -s --max-time 10 '%s'", url))
	if err != nil {
		ctx.Log().Error("failed to fetch weather", "error", err, "location", location)
		return fmt.Errorf("failed to fetch weather: %w", err)
	}

	if result.ExitCode != 0 {
		stderr := strings.TrimSpace(result.Stderr)
		if stderr == "" {
			stderr = fmt.Sprintf("curl exited with code %d", result.ExitCode)
		}
		ctx.Log().Warn("curl returned non-zero exit", "exit_code", result.ExitCode, "stderr", stderr)
		return fmt.Errorf("failed to fetch weather: %s", stderr)
	}

	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		ctx.Log().Warn("empty weather response", "location", location)
		return fmt.Errorf("no weather data returned")
	}

	ctx.Log().Debug("weather fetched successfully", "location", location, "output_size", len(output))
	fmt.Fprintln(ctx.Stdout(), output)
	return nil
}
