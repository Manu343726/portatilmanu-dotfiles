# weather

## Table of Contents

- [Services](#services)
  - [weather.WeatherService](#weatherweatherservice)
    - [Forecast](#forecast)
- [Messages](#messages)
  - [ForecastRequest](#forecastrequest)
  - [ForecastResponse](#forecastresponse)
- [Enums](#enums)
  - [WeatherFormat](#weatherformat)

## Services

### weather.WeatherService

WeatherService provides weather forecasts by querying wttr.in.
Supports both human-readable formatted output and structured JSON data
for plugin-to-plugin callers.

#### Forecast

Forecast fetches the weather forecast for a location.
Returns current conditions (temperature, humidity, wind, pressure, etc.)
from wttr.in. Use format="json" to get structured data for programmatic
consumption.

- **Request:** `weather.ForecastRequest`
- **Response:** `weather.ForecastResponse`


## Messages

### ForecastRequest

Request for a weather forecast for a specific location.

| Field | Type | Description |
|-------|------|-------------|
| `location` | string | Location to forecast (city name, "London", "Paris", or IP address). Supports any location that wttr.in understands. |
| `format` | weather.WeatherFormat | Output format for the forecast. Use JSON for programmatic consumption. |

### ForecastResponse

Response containing the weather forecast result.

| Field | Type | Description |
|-------|------|-------------|
| `report` | string | Raw forecast output from wttr.in. Content depends on format: - BRIEF: one-line text summary - FULL: multi-line detailed report - JSON: structured JSON for programmatic parsing |
| `exit_code` | int32 | Exit code from the curl command. 0 on success, non-zero on error. |
| `error_message` | string | Error message if the forecast could not be retrieved (network error, unknown location, etc.). |


## Enums

### WeatherFormat

Output format for weather forecast results.

| Name | Number | Description |
|------|--------|-------------|
| `WEATHER_FORMAT_UNSPECIFIED` | 0 | One-line summary of current conditions. |
| `WEATHER_FORMAT_BRIEF` | 1 | One-line text summary (default for human output). |
| `WEATHER_FORMAT_FULL` | 2 | Multi-line detailed weather report. |
| `WEATHER_FORMAT_JSON` | 3 | Structured JSON data for programmatic consumption. |

