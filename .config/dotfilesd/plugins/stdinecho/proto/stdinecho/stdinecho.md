# stdinecho

## Table of Contents

- [Services](#services)
  - [stdinecho.StdinechoService](#stdinechostdinechoservice)
    - [Echo](#echo)
- [Messages](#messages)
  - [EchoRequest](#echorequest)
  - [EchoResponse](#echoresponse)

## Services

### stdinecho.StdinechoService

StdinechoService reads lines from standard input and echoes them back.
Useful for testing stdin forwarding, interactive input pipelines, and
daemon-mediated I/O between CLI tools and plugins.

#### Echo

Echo reads up to N lines from stdin and returns them as a repeated
string. Each line is forwarded to stdout as it's received. Supports
RenderOutput mode for interactive echo feedback.

- **Request:** `stdinecho.EchoRequest`
- **Response:** `stdinecho.EchoResponse`


## Messages

### EchoRequest

EchoRequest specifies how many lines to read.

| Field | Type | Description |
|-------|------|-------------|
| `lines` | int32 | Number of lines to read from stdin before returning (default: 5). |

### EchoResponse

EchoResponse contains the lines that were read.

| Field | Type | Description |
|-------|------|-------------|
| `lines` | repeated string | Lines read from stdin, in order. |

