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
// When docsContent is set (pre-generated markdown from protoc-gen-docs,
// embedded via //go:embed at compile time), it serves that content directly.
// Otherwise it auto-generates docs from Config fields.
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
}

// GetDocumentation returns markdown-formatted documentation for a service
// or for the entire plugin.
func (s *documentationServiceServer) GetDocumentation(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.DocumentationRequest],
) (*connect.Response[dotfilesdv1.DocumentationResponse], error) {
	svcName := req.Msg.ServiceName

	if s.docsContent != "" {
		if svcName == "" {
			return connect.NewResponse(&dotfilesdv1.DocumentationResponse{
				Format:  "markdown",
				Content: s.docsContent,
			}), nil
		}
		section := extractSection(s.docsContent, "### "+svcName)
		if section == "" {
			return nil, connect.NewError(connect.CodeNotFound,
				fmt.Errorf("service %q not found", svcName))
		}
		return connect.NewResponse(&dotfilesdv1.DocumentationResponse{
			Format:  "markdown",
			Content: "# " + svcName + "\n\n" + section,
		}), nil
	}

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

// extractSection returns content between the heading starting with marker
// and the next heading at the same or lower level, or end of string.
func extractSection(md, marker string) string {
	idx := strings.Index(md, marker)
	if idx < 0 {
		return ""
	}
	rest := md[idx+len(marker):]
	eol := strings.Index(rest, "\n")
	if eol >= 0 {
		rest = rest[eol+1:]
	}
	end := -1
	for _, prefix := range []string{"\n### ", "\n## ", "\n# "} {
		if pos := strings.Index(rest, prefix); pos >= 0 && (end < 0 || pos < end) {
			end = pos
		}
	}
	if end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}
