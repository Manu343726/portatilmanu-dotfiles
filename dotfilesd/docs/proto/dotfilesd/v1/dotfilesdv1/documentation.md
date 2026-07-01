# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [DocumentationService](#documentationservice)
    - [GetDocumentation](#getdocumentation)
- [Messages](#messages)
  - [DocumentationRequest](#documentationrequest)
  - [DocumentationResponse](#documentationresponse)

## Services

### DocumentationService

DocumentationService provides human-readable documentation for plugin RPC
services. The daemon CLI and MCP tools use this to generate help text,
usage hints, and parameter descriptions dynamically.

Plugins get a default implementation via the SDK that returns "not
implemented". They can override it to provide rich, per-service docs.

#### GetDocumentation

GetDocumentation returns documentation for a specific service.

- **Request:** `dotfilesd.v1.DocumentationRequest`
- **Response:** `dotfilesd.v1.DocumentationResponse`


## Messages

### DocumentationRequest

| Field | Type | Description |
|-------|------|-------------|
| `service_name` | string | Fully-qualified service name (e.g. "weather.WeatherService"). If empty, returns documentation for ALL services exposed by this plugin. |

### DocumentationResponse

| Field | Type | Description |
|-------|------|-------------|
| `format` | string | Documentation format. Defaults to "markdown". Other formats may be supported in the future ("html", "man"). |
| `content` | string | Documentation content in the requested format. |

