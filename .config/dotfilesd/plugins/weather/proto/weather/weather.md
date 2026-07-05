# weather

## Table of Contents

- [Services](#services)
  - [weather.WeatherService](#weatherweatherservice)
    - [Forecast](#forecast)
- [Messages](#messages)
  - [ForecastRequest](#forecastrequest)
  - [ForecastResponse](#forecastresponse)

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
| `format` | string | Output format: "brief" (one-line summary), "full" (detailed), or "json" (structured data). Defaults to brief for human output, json for programmatic callers. |

### ForecastResponse

Response containing the weather forecast result.

| Field | Type | Description |
|-------|------|-------------|
| `report` | string | Raw forecast output from wttr.in. Content depends on format: - "brief": one-line text summary - "full": multi-line detailed report - "json": structured JSON for programmatic parsing |
| `exit_code` | int32 | Exit code from the curl command. 0 on success, non-zero on error. |
| `error_message` | string | Error message if the forecast could not be retrieved (network error, unknown location, etc.). |

