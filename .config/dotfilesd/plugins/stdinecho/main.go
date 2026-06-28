package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"dotfilesd/plugin"
	pb "plugins/stdinecho/proto/stdinecho"
	"plugins/stdinecho/proto/stdinecho/stdinechoconnect"

	"connectrpc.com/connect"
)

type echoServer struct{}

func (s *echoServer) Echo(
	ctx context.Context,
	req *connect.Request[pb.EchoRequest],
) (*connect.Response[pb.EchoResponse], error) {
	pc := plugin.ExtractContext(ctx)

	if pc == nil {
		return connect.NewResponse(&pb.EchoResponse{}), nil
	}

	pc.Log().Info("▶ Stdinecho.Echo",
		"render_output", pc.RenderOutput(),
		"lines", req.Msg.Lines,
	)

	maxLines := int(req.Msg.Lines)
	if maxLines <= 0 {
		maxLines = 5
	}

	var collected []string
	scanner := bufio.NewScanner(pc.Stdin())
	for scanner.Scan() {
		line := scanner.Text()
		collected = append(collected, line)
		pc.Log().Info("stdin line", "line", line, "read", len(collected))

		if pc.RenderOutput() {
			fmt.Fprintf(pc.Stdout(), "echo: %s\n", line)
		}

		if len(collected) >= maxLines {
			break
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		pc.Log().Error("stdin scan error", "error", err)
		collected = append(collected, fmt.Sprintf("error: %v", err))
	}

	result := strings.Join(collected, "\n")

	if pc.RenderOutput() {
		fmt.Fprintf(pc.Stdout(), "\n--- read %d lines ---\n--- result:\n%s\n---\n", len(collected), result)
	}

	return connect.NewResponse(&pb.EchoResponse{
		Lines: collected,
	}), nil
}

func main() {
	svc := &echoServer{}
	path, handler := stdinechoconnect.NewStdinechoServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "stdinecho",
		DisplayName: "Stdin Echo",
		Version:     "1.0.0",
		Description: "Reads from stdin and echoes back.",
		Services: []plugin.Service{
			{
				Name:        "stdinecho.StdinechoService",
				Description: "Read lines from stdin and echo them back",
				Path:        path,
				Handler:     handler,
			},
		},
	})
}
