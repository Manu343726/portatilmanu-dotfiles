// Package rpcreflection provides service discovery and dynamic RPC invocation
// for Connect RPC servers using HTTP-based gRPC reflection.
//
// It wraps connectrpc.com/grpcreflect for schema discovery and supports
// calling methods with raw JSON payloads or Go structs (serialised to JSON).
// The JSON encoding path works because Connect RPC servers natively accept
// both protobuf and JSON on the same HTTP endpoint.
package rpcreflection

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/grpcreflect"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// MethodInfo describes a single RPC method discovered via reflection.
type MethodInfo struct {
	ServiceName string                      // e.g. "weather.WeatherService"
	MethodName  string                      // e.g. "Forecast"
	InputMsg    protoreflect.MessageDescriptor
}

// ServiceInfo describes a discovered service and its methods.
type ServiceInfo struct {
	FullName string       // e.g. "weather.WeatherService"
	Methods  []MethodInfo
}

// Client discovers services and invokes methods on a Connect RPC server
// via HTTP-based gRPC reflection.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient returns a Client that talks to the Connect RPC server at the
// given base URL (e.g. "http://127.0.0.1:9105").
func NewClient(serverURL string) *Client {
	return &Client{
		baseURL:    serverURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// DiscoverServices uses HTTP-based gRPC reflection to list all services
// and their methods exposed by the server. It returns everything including
// built-in reflection services; callers should filter with IsSystemService
// if desired.
func (c *Client) DiscoverServices(ctx context.Context) ([]ServiceInfo, error) {
	refClient := grpcreflect.NewClient(c.httpClient, c.baseURL)
	stream := refClient.NewStream(ctx)
	defer stream.Close()

	svcNames, err := stream.ListServices()
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	var svcInfos []ServiceInfo
	for _, fullName := range svcNames {
		s := string(fullName)

		// Get file descriptor(s) containing this service.
		fdProtos, err := stream.FileContainingSymbol(fullName)
		if err != nil {
			slog.Debug("FileContainingSymbol failed", "service", s, "error", err)
			continue
		}

		// Build a *protoregistry.Files from all returned descriptors.
		svcDesc, err := findServiceDescriptor(fdProtos, s)
		if err != nil {
			slog.Debug("findServiceDescriptor failed", "service", s, "error", err)
			continue
		}

		var methods []MethodInfo
		for i := 0; i < svcDesc.Methods().Len(); i++ {
			md := svcDesc.Methods().Get(i)
			methods = append(methods, MethodInfo{
				ServiceName: s,
				MethodName:  string(md.Name()),
				InputMsg:    md.Input(),
			})
		}
		svcInfos = append(svcInfos, ServiceInfo{FullName: s, Methods: methods})
	}
	return svcInfos, nil
}

// CallJSON invokes the given RPC method with a JSON payload and returns the
// raw JSON response bytes. It POSTs to the Connect JSON endpoint at
// {baseURL}/{service}/{method} with Content-Type: application/json.
func (c *Client) CallJSON(ctx context.Context, method MethodInfo, payload json.RawMessage) (json.RawMessage, error) {
	rpcURL := fmt.Sprintf("%s/%s/%s", c.baseURL, method.ServiceName, method.MethodName)

	req, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("RPC failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return json.RawMessage(respBody), nil
}

// CallStruct invokes the given RPC method by serialising req to JSON and
// deserialising the response into resp (which must be a pointer).
//
// This is a convenience wrapper around CallJSON for callers that already
// have Go values rather than raw JSON.
func (c *Client) CallStruct(ctx context.Context, method MethodInfo, req, resp any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	raw, err := c.CallJSON(ctx, method, body)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(raw, resp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	return nil
}

// IsSystemService returns true if name is a gRPC server reflection service
// that should typically be excluded from CLI/MCP tool generation.
func IsSystemService(name string) bool {
	return name == "grpc.reflection.v1.ServerReflection" ||
		name == "grpc.reflection.v1alpha.ServerReflection"
}

// findServiceDescriptor builds a *protoregistry.Files from one or more
// FileDescriptorProtos and looks up the service by its fully-qualified name.
func findServiceDescriptor(fdProtos []*descriptorpb.FileDescriptorProto, svcFullName string) (protoreflect.ServiceDescriptor, error) {
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: fdProtos})
	if err != nil {
		return nil, fmt.Errorf("build file descriptors: %w", err)
	}

	d, err := files.FindDescriptorByName(protoreflect.FullName(svcFullName))
	if err != nil {
		return nil, fmt.Errorf("find service %q: %w", svcFullName, err)
	}

	svcDesc, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("symbol %q is not a service", svcFullName)
	}
	return svcDesc, nil
}
