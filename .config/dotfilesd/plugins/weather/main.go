// Weather plugin — demonstrates the dotfilesd plugin SDK.
//
// Provides a "forecast" tool that fetches weather data for a given location
// by using the daemon's exec context to call out to wttr.in via curl.
//
// This plugin showcases all major SDK features:
//   - Tool definition with input schema
//   - Shell command execution via Context.Exec()
//   - Real-time streaming output via ctx.Stdout() / ctx.Stderr()
//   - Error handling
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"dotfilesd/plugin"
)

func main() {
	plugin.Serve("weather", "Weather", "1.0.0", "Weather forecast plugin using wttr.in",
		plugin.NewTool("forecast", "Get weather forecast for a location",
			plugin.ToolInputSchema{
				Properties: map[string]plugin.PropertySchema{
					"location": {
						Type:        "string",
						Description: "Location to get weather for (city name, ZIP code, IP address, etc.)",
					},
					"format": {
						Type:        "string",
						Description: "Output format (brief, full, json)",
						Default:     "brief",
					},
				},
				Required: []string{"location"},
			},
			plugin.CLIHints{
				CommandPath: "weather forecast",
				Category:    "utilities",
			},
			forecastFn,
		),
	)
}

// forecastFn is the tool implementation for the "forecast" tool.
func forecastFn(ctx plugin.Context, args map[string]string) error {
	location := args["location"]
	if location == "" {
		return fmt.Errorf("location is required")
	}

	format := args["format"]
	if format == "" {
		format = "brief"
	}

	// Build the wttr.in URL.
	// - "brief" uses the default short format (curl wttr.in/Location?0)
	// - "full" uses the full format (curl wttr.in/Location)
	// - "json" returns JSON data (curl wttr.in/Location?format=j1)
	var url string
	switch format {
	case "json":
		url = fmt.Sprintf("wttr.in/%s?format=j1", location)
	case "full":
		url = fmt.Sprintf("wttr.in/%s", location)
	default:
		url = fmt.Sprintf("wttr.in/%s?0", location)
	}

	// Fetch weather data via curl using the daemon's exec context.
	result, err := ctx.Exec(fmt.Sprintf("curl -s --max-time 10 '%s'", url))
	if err != nil {
		return fmt.Errorf("failed to fetch weather: %w", err)
	}

	if result.ExitCode != 0 {
		stderr := strings.TrimSpace(result.Stderr)
		if stderr == "" {
			stderr = fmt.Sprintf("curl exited with code %d", result.ExitCode)
		}
		return fmt.Errorf("failed to fetch weather: %s", stderr)
	}

	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		return fmt.Errorf("no weather data returned")
	}

	// For JSON format, pretty-print and also write structured data.
	if format == "json" {
		var data any
		if err := json.Unmarshal([]byte(output), &data); err == nil {
			pretty, _ := json.MarshalIndent(data, "", "  ")
			fmt.Fprintln(ctx.Stdout(), string(pretty))
			return nil
		}
	}

	fmt.Fprintln(ctx.Stdout(), output)
	return nil
}
