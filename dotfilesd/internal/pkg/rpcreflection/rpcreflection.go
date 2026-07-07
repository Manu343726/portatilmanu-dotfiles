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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/grpcreflect"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// MethodInfo describes a single RPC method discovered via reflection.
type MethodInfo struct {
	ServiceName string // e.g. "weather.WeatherService"
	MethodName  string // e.g. "Forecast"
	InputMsg    protoreflect.MessageDescriptor
	OutputMsg   protoreflect.MessageDescriptor
}

// ServiceInfo describes a discovered service and its methods.
type ServiceInfo struct {
	FullName string // e.g. "weather.WeatherService"
	Methods  []MethodInfo
	// RawFdProtos holds serialized google.protobuf.FileDescriptorProto messages
	// for this service and its dependencies. Clients can reconstruct
	// protoreflect descriptors without direct grpcreflect access.
	RawFdProtos [][]byte
	// Descriptor is the live protoreflect.ServiceDescriptor.
	// Populated by DiscoverServices and BuildServiceInfoFromProtos.
	// Used by BuildServiceSchema to extract source comments.
	Descriptor protoreflect.ServiceDescriptor
}

// Client discovers services and invokes methods on a Connect RPC server
// via HTTP-based gRPC reflection.
type Client struct {
	baseURL    string
	httpClient *http.Client // HTTP/1.1+ client for Connect JSON calls
	grpcClient *http.Client // HTTP/2 (h2c) client for gRPC reflection
}

// NewClient returns a Client that talks to the Connect RPC server at the
// given base URL (e.g. "http://127.0.0.1:9105").
//
// It creates two transports internally: one for standard HTTP requests
// (Connect JSON RPC, HTTP/1.1+) and one for HTTP/2 cleartext (gRPC
// reflection protocol which requires HTTP/2).
func NewClient(serverURL string) *Client {
	return &Client{
		baseURL:    serverURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		grpcClient: &http.Client{
			Transport: &http2.Transport{
				AllowHTTP: true,
				DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, network, addr)
				},
			},
			Timeout: 10 * time.Second,
		},
	}
}

// DiscoverServices uses HTTP-based gRPC reflection to list all services
// and their methods exposed by the server. It returns everything including
// built-in reflection services; callers should filter with IsSystemService
// if desired.
func (c *Client) DiscoverServices(ctx context.Context) ([]ServiceInfo, error) {
	refClient := grpcreflect.NewClient(c.grpcClient, c.baseURL)
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
				OutputMsg:   md.Output(),
			})
		}

		// Serialize the raw FileDescriptorProtos for downstream caching.
		var rawFds [][]byte
		for _, fdp := range fdProtos {
			b, err := proto.Marshal(fdp)
			if err != nil {
				slog.Debug("marshal FileDescriptorProto failed", "service", s, "error", err)
				continue
			}
			rawFds = append(rawFds, b)
		}

		svcInfos = append(svcInfos, ServiceInfo{FullName: s, Methods: methods, RawFdProtos: rawFds, Descriptor: svcDesc})
	}
	return svcInfos, nil
}

// CallJSON invokes the given RPC method with a JSON payload and returns the
// raw JSON response bytes. It POSTs to the Connect JSON endpoint at
// {baseURL}/{service}/{method} with Content-Type: application/json.
func (c *Client) CallJSON(ctx context.Context, method MethodInfo, payload json.RawMessage) (json.RawMessage, error) {
	return c.CallJSONWithHeaders(ctx, method, payload, nil)
}

// CallJSONWithHeaders is like CallJSON but allows setting additional HTTP
// headers on the request. Headers are merged into the request; they cannot
// override Content-Type.
func (c *Client) CallJSONWithHeaders(ctx context.Context, method MethodInfo, payload json.RawMessage, headers map[string]string) (json.RawMessage, error) {
	rpcURL := fmt.Sprintf("%s/%s/%s", c.baseURL, method.ServiceName, method.MethodName)

	req, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

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

// BuildServiceInfoFromProtos reconstructs a ServiceInfo from serialized
// FileDescriptorProto blobs. It deserializes the protos, finds the service
// descriptor by name, extracts its methods, and returns a ServiceInfo with
// live protoreflect.MessageDescriptor handles.
//
// This is the inverse of DiscoverServices — it lets clients reconstruct the
// same ServiceInfo without making a live grpcreflect call.
func BuildServiceInfoFromProtos(svcFullName string, rawFds [][]byte) (ServiceInfo, error) {
	fdProtos := make([]*descriptorpb.FileDescriptorProto, 0, len(rawFds))
	for _, raw := range rawFds {
		var fdp descriptorpb.FileDescriptorProto
		if err := proto.Unmarshal(raw, &fdp); err != nil {
			return ServiceInfo{}, fmt.Errorf("unmarshal FileDescriptorProto: %w", err)
		}
		fdProtos = append(fdProtos, &fdp)
	}

	svcDesc, err := findServiceDescriptor(fdProtos, svcFullName)
	if err != nil {
		return ServiceInfo{}, fmt.Errorf("find service %q: %w", svcFullName, err)
	}

	var methods []MethodInfo
	for i := 0; i < svcDesc.Methods().Len(); i++ {
		md := svcDesc.Methods().Get(i)
		methods = append(methods, MethodInfo{
			ServiceName: svcFullName,
			MethodName:  string(md.Name()),
			InputMsg:    md.Input(),
			OutputMsg:   md.Output(),
		})
	}

	return ServiceInfo{
		FullName:    svcFullName,
		Methods:     methods,
		RawFdProtos: rawFds,
		Descriptor:  svcDesc,
	}, nil
}

// BuildServiceInfosFromProtos reconstructs multiple ServiceInfos from a
// flat set of serialized FileDescriptorProto blobs and a list of service
// names. Use this when you have a plugin's full file_descriptor_set from
// the registry and want to rebuild all service descriptors.
func BuildServiceInfosFromProtos(svcNames []string, rawFds [][]byte) []ServiceInfo {
	var infos []ServiceInfo
	for _, svcName := range svcNames {
		info, err := BuildServiceInfoFromProtos(svcName, rawFds)
		if err != nil {
			slog.Debug("BuildServiceInfoFromProtos failed", "service", svcName, "error", err)
			continue
		}
		infos = append(infos, info)
	}
	return infos
}

// ─────────────────────────────────────────────
// ServiceSchema builders (ServiceInfo → proto)
// ─────────────────────────────────────────────

// BuildServiceSchema converts a ServiceInfo (backed by live protoreflect
// descriptors) into a *dotfilesdv1.ServiceSchema proto message. The schema
// contains full recursive type metadata (messages, fields, enums) suitable
// for CLI flag generation and MCP tool schema construction without further
// grpcreflect calls.
func BuildServiceSchema(svc *ServiceInfo) *dotfilesdv1.ServiceSchema {
	schema := &dotfilesdv1.ServiceSchema{
		Name:    svc.FullName,
		Methods: make([]*dotfilesdv1.MethodSchema, 0, len(svc.Methods)),
	}

	visited := make(map[string]bool)
	for _, m := range svc.Methods {
		ms := &dotfilesdv1.MethodSchema{
			Name:     m.MethodName,
			Request:  buildMessageSchema(m.InputMsg, visited),
			Response: buildMessageSchema(m.OutputMsg, visited),
		}
		schema.Methods = append(schema.Methods, ms)
	}
	return schema
}

// BuildServiceSchemas converts a slice of ServiceInfo into a slice of
// *dotfilesdv1.ServiceSchema.
func BuildServiceSchemas(svcs []ServiceInfo) []*dotfilesdv1.ServiceSchema {
	out := make([]*dotfilesdv1.ServiceSchema, 0, len(svcs))
	for i := range svcs {
		out = append(out, BuildServiceSchema(&svcs[i]))
	}
	return out
}

// buildMessageSchema recursively converts a protoreflect.MessageDescriptor
// into a *dotfilesdv1.MessageSchema, inlining nested messages and enums.
// The visited map prevents infinite recursion on circular message references.
func buildMessageSchema(md protoreflect.MessageDescriptor, visited map[string]bool) *dotfilesdv1.MessageSchema {
	name := string(md.FullName())
	if visited[name] {
		return &dotfilesdv1.MessageSchema{Name: name}
	}
	visited[name] = true

	schema := &dotfilesdv1.MessageSchema{
		Name:   name,
		Fields: make([]*dotfilesdv1.FieldSchema, 0, md.Fields().Len()),
		Enums:  make([]*dotfilesdv1.EnumSchema, 0),
		Messages: make([]*dotfilesdv1.MessageSchema, 0),
	}

	// Nested enums.
	for i := 0; i < md.Enums().Len(); i++ {
		ed := md.Enums().Get(i)
		vals := make([]*dotfilesdv1.EnumValue, ed.Values().Len())
		for j := 0; j < ed.Values().Len(); j++ {
			v := ed.Values().Get(j)
			vals[j] = &dotfilesdv1.EnumValue{
				Name:   string(v.Name()),
				Number: int32(v.Number()),
			}
		}
		schema.Enums = append(schema.Enums, &dotfilesdv1.EnumSchema{
			Name:   string(ed.FullName()),
			Values: vals,
		})
	}

	// Nested messages (recursive).
	for i := 0; i < md.Messages().Len(); i++ {
		nested := md.Messages().Get(i)
		schema.Messages = append(schema.Messages, buildMessageSchema(nested, visited))
	}

	// Fields — also collect message types referenced by fields (e.g. a field
	// "RAMSnapshot ram" references a type defined elsewhere in the proto).
	// These are appended to Messages so clients can look them up by typeName.
	for i := 0; i < md.Fields().Len(); i++ {
		fd := md.Fields().Get(i)
		if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
			refMsg := fd.Message()
			if refMsg != nil {
				refName := string(refMsg.FullName())
				if !visited[refName] {
					schema.Messages = append(schema.Messages, buildMessageSchema(refMsg, visited))
				}
			}
		}
	}

	// Build field schemas.
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		fs := &dotfilesdv1.FieldSchema{
			Name:     string(fd.Name()),
			Kind:     kindToProto(fd.Kind()),
			Label:    labelToProto(fd),
			TypeName: typeNameString(fd),
		}
		// Inline enum schema so clients have choices without cross-referencing.
		if fd.Kind() == protoreflect.EnumKind {
			ed := fd.Enum()
			vals := make([]*dotfilesdv1.EnumValue, ed.Values().Len())
			for j := 0; j < ed.Values().Len(); j++ {
				v := ed.Values().Get(j)
				vals[j] = &dotfilesdv1.EnumValue{
					Name:   string(v.Name()),
					Number: int32(v.Number()),
				}
			}
			fs.EnumSchema = &dotfilesdv1.EnumSchema{
				Name:   string(ed.FullName()),
				Values: vals,
			}
		}
		schema.Fields = append(schema.Fields, fs)
	}

	return schema
}

// kindToProto maps protoreflect.Kind → dotfilesdv1.FieldKind.
func kindToProto(k protoreflect.Kind) dotfilesdv1.FieldKind {
	switch k {
	case protoreflect.DoubleKind:
		return dotfilesdv1.FieldKind_FIELD_KIND_DOUBLE
	case protoreflect.FloatKind:
		return dotfilesdv1.FieldKind_FIELD_KIND_FLOAT
	case protoreflect.Int64Kind:
		return dotfilesdv1.FieldKind_FIELD_KIND_INT64
	case protoreflect.Uint64Kind:
		return dotfilesdv1.FieldKind_FIELD_KIND_UINT64
	case protoreflect.Int32Kind:
		return dotfilesdv1.FieldKind_FIELD_KIND_INT32
	case protoreflect.Fixed64Kind:
		return dotfilesdv1.FieldKind_FIELD_KIND_FIXED64
	case protoreflect.Fixed32Kind:
		return dotfilesdv1.FieldKind_FIELD_KIND_FIXED32
	case protoreflect.BoolKind:
		return dotfilesdv1.FieldKind_FIELD_KIND_BOOL
	case protoreflect.StringKind:
		return dotfilesdv1.FieldKind_FIELD_KIND_STRING
	case protoreflect.BytesKind:
		return dotfilesdv1.FieldKind_FIELD_KIND_BYTES
	case protoreflect.Uint32Kind:
		return dotfilesdv1.FieldKind_FIELD_KIND_UINT32
	case protoreflect.Sfixed32Kind:
		return dotfilesdv1.FieldKind_FIELD_KIND_SFIXED32
	case protoreflect.Sfixed64Kind:
		return dotfilesdv1.FieldKind_FIELD_KIND_SFIXED64
	case protoreflect.Sint32Kind:
		return dotfilesdv1.FieldKind_FIELD_KIND_SINT32
	case protoreflect.Sint64Kind:
		return dotfilesdv1.FieldKind_FIELD_KIND_SINT64
	case protoreflect.EnumKind:
		return dotfilesdv1.FieldKind_FIELD_KIND_ENUM
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return dotfilesdv1.FieldKind_FIELD_KIND_MESSAGE
	default:
		return dotfilesdv1.FieldKind_FIELD_KIND_UNSPECIFIED
	}
}

// labelToProto maps a field descriptor's cardinality to FieldLabel.
func labelToProto(fd protoreflect.FieldDescriptor) dotfilesdv1.FieldLabel {
	if fd.IsList() || fd.IsMap() {
		return dotfilesdv1.FieldLabel_FIELD_LABEL_REPEATED
	}
	if fd.HasOptionalKeyword() {
		return dotfilesdv1.FieldLabel_FIELD_LABEL_OPTIONAL
	}
	return dotfilesdv1.FieldLabel_FIELD_LABEL_OPTIONAL
}

// typeNameString returns the fully-qualified type name for message/enum
// fields, or empty string for scalar types.
func typeNameString(fd protoreflect.FieldDescriptor) string {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return string(fd.Message().FullName())
	case protoreflect.EnumKind:
		return string(fd.Enum().FullName())
	default:
		return ""
	}
}


