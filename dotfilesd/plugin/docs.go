package plugin

import (
	"context"
	"fmt"
	"strings"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// documentationServiceServer implements the default DocumentationService.
//
// When DocsProto is set (pre-generated Documentation proto from protoc-gen-docs,
// embedded via //go:embed at compile time), it returns structured docs in the
// documentation field of the response. Otherwise it auto-generates docs from
// Config fields.
//
// Plugins can override by providing their own DocumentationService in
// Config.Services with the name "dotfilesd.v1.DocumentationService". If
// the SDK detects a user-provided DocumentationService, it skips mounting
// the default one.
type documentationServiceServer struct {
	name        string
	displayName string
	version     string
	description string
	services    []Service
	docsContent string
	docsProto   *dotfilesdv1.Documentation
}

// GetDocumentation returns documentation for a service or the entire plugin.
func (s *documentationServiceServer) GetDocumentation(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.DocumentationRequest],
) (*connect.Response[dotfilesdv1.DocumentationResponse], error) {
	svcName := req.Msg.ServiceName

	// Structured proto docs take precedence.
	if s.docsProto != nil {
		return s.serveProto(svcName)
	}

	// Legacy markdown embedded content.
	if s.docsContent != "" {
		return connect.NewResponse(&dotfilesdv1.DocumentationResponse{
			Format:  "markdown",
			Content: s.docsContent,
		}), nil
	}

	// Auto-generate from Config fields.
	if svcName == "" {
		return s.pluginDocs()
	}

	for _, svc := range s.services {
		if svc.Name == svcName {
			return s.serviceDocs(svc)
		}
	}

	return nil, connect.NewError(connect.CodeNotFound,
		fmt.Errorf("service %q not found", svcName))
}

func (s *documentationServiceServer) serveProto(svcName string) (*connect.Response[dotfilesdv1.DocumentationResponse], error) {
	resp := &dotfilesdv1.DocumentationResponse{}

	if svcName == "" {
		resp.Documentation = s.docsProto
		return connect.NewResponse(resp), nil
	}

	for _, svc := range s.docsProto.Services {
		if svc.Name == svcName {
			resp.Documentation = &dotfilesdv1.Documentation{
				Package:     s.docsProto.Package,
				Description: s.docsProto.Description,
				Services:    []*dotfilesdv1.ServiceDoc{svc},
			}
			return connect.NewResponse(resp), nil
		}
	}

	return nil, connect.NewError(connect.CodeNotFound,
		fmt.Errorf("service %q not found", svcName))
}

func (s *documentationServiceServer) pluginDocs() (*connect.Response[dotfilesdv1.DocumentationResponse], error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", s.displayName)
	fmt.Fprintf(&b, "**Version:** %s\n\n", s.version)
	if s.description != "" {
		fmt.Fprintf(&b, "%s\n\n", s.description)
	}
	if len(s.services) > 0 {
		fmt.Fprintf(&b, "## Services\n\n")
		for _, svc := range s.services {
			fmt.Fprintf(&b, "- **`%s`**: %s\n", svc.Name, svc.Description)
		}
	}
	return connect.NewResponse(&dotfilesdv1.DocumentationResponse{
		Format:  "markdown",
		Content: b.String(),
	}), nil
}

func (s *documentationServiceServer) serviceDocs(svc Service) (*connect.Response[dotfilesdv1.DocumentationResponse], error) {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", svc.Name)
	if svc.Description != "" {
		fmt.Fprintf(&b, "%s\n", svc.Description)
	}
	return connect.NewResponse(&dotfilesdv1.DocumentationResponse{
		Format:  "markdown",
		Content: b.String(),
	}), nil
}
