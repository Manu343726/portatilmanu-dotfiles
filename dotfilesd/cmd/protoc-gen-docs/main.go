package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Stderr.WriteString("read stdin: " + err.Error() + "\n")
		os.Exit(1)
	}

	req := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(data, req); err != nil {
		os.Stderr.WriteString("unmarshal request: " + err.Error() + "\n")
		os.Exit(1)
	}

	var resp pluginpb.CodeGeneratorResponse

	// Only generate docs for proto files that are directly requested for
	// compilation (not their transitive imports like google/protobuf/*.proto).
	requested := make(map[string]bool, len(req.GetFileToGenerate()))
	for _, f := range req.GetFileToGenerate() {
		requested[f] = true
	}

	for _, fdp := range req.GetProtoFile() {
		if !requested[fdp.GetName()] {
			continue
		}
		md := renderFile(fdp)
		if md != "" {
			outName := protoDocOutputName(fdp.GetName())
			resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
				Name:    proto.String(outName),
				Content: proto.String(md),
			})
		}

		// Generate Go embed file for the markdown docs.
		goPkg := goPackageName(fdp)
		if goPkg == "" {
			continue
		}
		goOutName := protoDocGoEmbedName(fdp.GetName())
		goContent := renderGoEmbed(fdp.GetName(), goPkg)
		resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
			Name:    proto.String(goOutName),
			Content: proto.String(goContent),
		})
	}

	if resp.File == nil {
		resp.File = []*pluginpb.CodeGeneratorResponse_File{}
	}

	out, err := proto.Marshal(&resp)
	if err != nil {
		os.Stderr.WriteString("marshal response: " + err.Error() + "\n")
		os.Exit(1)
	}
	os.Stdout.Write(out)
}

// --------------------------------------------------------------------------
// rendering
// --------------------------------------------------------------------------

func renderFile(fdp *descriptorpb.FileDescriptorProto) string {
	pkg := fdp.GetPackage()
	if pkg == "" {
		pkg = fdp.GetOptions().GetGoPackage()
	}
	if pkg == "" {
		pkg = "(unnamed)"
	}

	src := newSourceInfo(fdp)
	svcs := fdp.GetService()
	msgs := fdp.GetMessageType()
	enums := fdp.GetEnumType()

	if len(svcs) == 0 && len(msgs) == 0 && len(enums) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(fmt.Sprintf("# %s\n\n", pkg))
	src.fileComment(&b)

	// table of contents
	if len(svcs) > 0 || len(msgs) > 0 || len(enums) > 0 {
		b.WriteString("## Table of Contents\n\n")
		if len(svcs) > 0 {
			b.WriteString("- [Services](#services)\n")
			for _, svc := range svcs {
				anchor := svcAnchor(svc.GetName())
				b.WriteString(fmt.Sprintf("  - [%s](#%s)\n", svc.GetName(), anchor))
				for _, m := range svc.GetMethod() {
					mAnchor := methodAnchor(svc.GetName(), m.GetName())
					b.WriteString(fmt.Sprintf("    - [%s](#%s)\n", m.GetName(), mAnchor))
				}
			}
		}
		if len(msgs) > 0 {
			b.WriteString("- [Messages](#messages)\n")
			for _, msg := range msgs {
				anchor := messageAnchor(msg.GetName())
				b.WriteString(fmt.Sprintf("  - [%s](#%s)\n", msg.GetName(), anchor))
			}
		}
		if len(enums) > 0 {
			b.WriteString("- [Enums](#enums)\n")
			for _, e := range enums {
				anchor := enumAnchor(e.GetName())
				b.WriteString(fmt.Sprintf("  - [%s](#%s)\n", e.GetName(), anchor))
			}
		}
		b.WriteString("\n")
	}

	if len(svcs) > 0 {
		b.WriteString("## Services\n\n")
		for i, svc := range svcs {
			path := []int32{fieldService, int32(i)}
			renderService(&b, src, path, svc)
		}
	}

	if len(msgs) > 0 {
		b.WriteString("\n## Messages\n\n")
		for i, msg := range msgs {
			path := []int32{fieldMessage, int32(i)}
			renderMessage(&b, src, path, msg, 1)
		}
	}

	if len(enums) > 0 {
		b.WriteString("\n## Enums\n\n")
		for i, e := range enums {
			path := []int32{fieldEnum, int32(i)}
			renderEnum(&b, src, path, e, 1)
		}
	}

	return b.String()
}

func renderService(b *strings.Builder, src *sourceInfo, path []int32, svc *descriptorpb.ServiceDescriptorProto) {
	b.WriteString(fmt.Sprintf("### %s\n\n", svc.GetName()))
	src.comment(b, path)

	for i, m := range svc.GetMethod() {
		methodPath := append(path, fieldMethod, int32(i))
		renderMethod(b, src, methodPath, m)
	}
}

func renderMethod(b *strings.Builder, src *sourceInfo, path []int32, m *descriptorpb.MethodDescriptorProto) {
	b.WriteString(fmt.Sprintf("#### %s\n\n", m.GetName()))
	src.comment(b, path)

	inputType := trimPrefixDot(m.GetInputType())
	outputType := trimPrefixDot(m.GetOutputType())

	b.WriteString(fmt.Sprintf("- **Request:** `%s`\n", inputType))
	b.WriteString(fmt.Sprintf("- **Response:** `%s`\n\n", outputType))
}

func renderMessage(b *strings.Builder, src *sourceInfo, path []int32, msg *descriptorpb.DescriptorProto, level int) {
	heading := strings.Repeat("#", level+2)
	b.WriteString(fmt.Sprintf("%s %s\n\n", heading, msg.GetName()))
	src.comment(b, path)

	// nested messages
	for i, nested := range msg.GetNestedType() {
		nestedPath := append(path, fieldNestedMsg, int32(i))
		b.WriteString("\n")
		renderMessage(b, src, nestedPath, nested, level+1)
	}

	// nested enums
	for i, e := range msg.GetEnumType() {
		enumPath := append(path, fieldNestedEnum, int32(i))
		renderEnum(b, src, enumPath, e, level+1)
	}

	if len(msg.GetField()) == 0 && len(msg.GetOneofDecl()) == 0 {
		return
	}

	b.WriteString("| Field | Type | Description |\n")
	b.WriteString("|-------|------|-------------|\n")
	for i, f := range msg.GetField() {
		fieldPath := append(path, fieldField, int32(i))
		renderFieldRow(b, src, fieldPath, f)
	}
	for i, o := range msg.GetOneofDecl() {
		oneofPath := append(path, fieldOneof, int32(i))
		b.WriteString(fmt.Sprintf("| `%s` | oneof | ", o.GetName()))
		src.commentInline(b, oneofPath)
		b.WriteString(" |\n")
	}
	b.WriteString("\n")
}

func renderEnum(b *strings.Builder, src *sourceInfo, path []int32, e *descriptorpb.EnumDescriptorProto, level int) {
	heading := strings.Repeat("#", level+2)
	b.WriteString(fmt.Sprintf("%s %s\n\n", heading, e.GetName()))
	src.comment(b, path)

	if len(e.GetValue()) == 0 {
		return
	}

	b.WriteString("| Name | Number | Description |\n")
	b.WriteString("|------|--------|-------------|\n")
	for i, v := range e.GetValue() {
		valPath := append(path, fieldEnumVal, int32(i))
		b.WriteString(fmt.Sprintf("| `%s` | %d | ", v.GetName(), v.GetNumber()))
		src.commentInline(b, valPath)
		b.WriteString(" |\n")
	}
	b.WriteString("\n")
}

func renderFieldRow(b *strings.Builder, src *sourceInfo, path []int32, f *descriptorpb.FieldDescriptorProto) {
	b.WriteString(fmt.Sprintf("| `%s` | %s | ", f.GetName(), fieldType(f)))
	src.commentInline(b, path)
	b.WriteString(" |\n")
}

func fieldType(f *descriptorpb.FieldDescriptorProto) string {
	label := ""
	if f.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
		if f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
			typeName := trimPrefixDot(f.GetTypeName())
			if strings.HasSuffix(typeName, "Entry") {
				return "map<...>"
			}
		}
		label = "repeated "
	}
	if f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE ||
		f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
		return label + trimPrefixDot(f.GetTypeName())
	}
	return label + scalarType(f.GetType())
}

func trimPrefixDot(s string) string {
	return strings.TrimPrefix(s, ".")
}

// protoDocOutputName converts a proto file path to a markdown file path.
// It preserves the directory structure so the output mirrors the proto
// source layout: proto/dotfilesd/v1/foo.proto → proto/dotfilesd/v1/foo.md.
func protoDocOutputName(protoName string) string {
	return strings.TrimSuffix(protoName, ".proto") + ".md"
}

// protoDocGoEmbedName converts a proto file path to the Go embed helper path.
// proto/foo/foo.proto → proto/foo/foo_docs.go
func protoDocGoEmbedName(protoName string) string {
	return strings.TrimSuffix(protoName, ".proto") + "_docs.go"
}

// goPackageName extracts the Go package name from a FileDescriptorProto.
// It uses the go_package option: if present, the part after ';' is preferred,
// otherwise the last path component after '/'.
func goPackageName(fdp *descriptorpb.FileDescriptorProto) string {
	gopkg := fdp.GetOptions().GetGoPackage()
	if gopkg == "" {
		// Fallback: derive from directory name.
		name := fdp.GetName()
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		return strings.TrimSuffix(name, ".proto")
	}
	if idx := strings.Index(gopkg, ";"); idx >= 0 {
		return gopkg[idx+1:]
	}
	if idx := strings.LastIndex(gopkg, "/"); idx >= 0 {
		return gopkg[idx+1:]
	}
	return gopkg
}

// renderGoEmbed generates a Go source file that embeds the generated markdown
// documentation. Plugin main.go files import this and pass to Config.DocsContent.
func renderGoEmbed(protoName, pkg string) string {
	mdName := strings.TrimSuffix(protoName, ".proto") + ".md"
	var b strings.Builder
	b.WriteString("// Code generated by protoc-gen-docs. DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("import _ \"embed\"\n\n")
	b.WriteString("//go:embed " + mdName + "\n")
	b.WriteString("var PluginDocs string\n")
	return b.String()
}

// gfmAnchor generates a GitHub-Flavored Markdown heading anchor.
// Algorithm: lowercase, strip non-alphanumeric (except spaces/hyphens),
// replace spaces with hyphens, collapse runs.
func gfmAnchor(heading string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(heading) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == ' ' {
			if r == ' ' || r == '-' {
				if prevDash {
					continue
				}
				prevDash = true
				b.WriteByte('-')
			} else {
				prevDash = false
				b.WriteRune(r)
			}
		} else {
			// skip non-alphanumeric, non-space, non-hyphen
		}
	}
	return strings.Trim(b.String(), "-")
}

func svcAnchor(name string) string { return gfmAnchor(name) }
func methodAnchor(svc, method string) string { return gfmAnchor(method) }
func messageAnchor(name string) string { return gfmAnchor(name) }
func enumAnchor(name string) string { return gfmAnchor(name) }

func scalarType(typ descriptorpb.FieldDescriptorProto_Type) string {
	switch typ {
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE:
		return "double"
	case descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
		return "float"
	case descriptorpb.FieldDescriptorProto_TYPE_INT64:
		return "int64"
	case descriptorpb.FieldDescriptorProto_TYPE_UINT64:
		return "uint64"
	case descriptorpb.FieldDescriptorProto_TYPE_INT32:
		return "int32"
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED64:
		return "fixed64"
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED32:
		return "fixed32"
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		return "bool"
	case descriptorpb.FieldDescriptorProto_TYPE_STRING:
		return "string"
	case descriptorpb.FieldDescriptorProto_TYPE_GROUP:
		return "group"
	case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE:
		return "message"
	case descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		return "bytes"
	case descriptorpb.FieldDescriptorProto_TYPE_UINT32:
		return "uint32"
	case descriptorpb.FieldDescriptorProto_TYPE_ENUM:
		return "enum"
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED32:
		return "sfixed32"
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
		return "sfixed64"
	case descriptorpb.FieldDescriptorProto_TYPE_SINT32:
		return "sint32"
	case descriptorpb.FieldDescriptorProto_TYPE_SINT64:
		return "sint64"
	default:
		return typ.String()
	}
}

// --------------------------------------------------------------------------
// source code info helpers
// --------------------------------------------------------------------------

type sourceInfo struct {
	locs map[string]*descriptorpb.SourceCodeInfo_Location
}

func pathKey(path []int32) string {
	ss := make([]string, len(path))
	for i, p := range path {
		ss[i] = fmt.Sprintf("%d", p)
	}
	return strings.Join(ss, ".")
}

func newSourceInfo(fdp *descriptorpb.FileDescriptorProto) *sourceInfo {
	si := &sourceInfo{locs: map[string]*descriptorpb.SourceCodeInfo_Location{}}
	sc := fdp.GetSourceCodeInfo()
	if sc == nil {
		return si
	}
	for _, loc := range sc.GetLocation() {
		si.locs[pathKey(loc.GetPath())] = loc
	}
	return si
}

func cleanComment(text string) string {
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		cleaned = append(cleaned, trimmed)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func (si *sourceInfo) comment(b *strings.Builder, path []int32) {
	loc := si.locs[pathKey(path)]
	if loc == nil {
		return
	}
	c := cleanComment(loc.GetLeadingComments())
	if c == "" {
		return
	}
	b.WriteString(c)
	b.WriteString("\n\n")
}

func (si *sourceInfo) commentInline(b *strings.Builder, path []int32) {
	loc := si.locs[pathKey(path)]
	if loc == nil {
		return
	}
	c := cleanComment(loc.GetLeadingComments())
	c = strings.ReplaceAll(c, "\n", " ")
	if c != "" {
		b.WriteString(c)
	}
}

func (si *sourceInfo) fileComment(b *strings.Builder) {
	loc := si.locs["12"]
	if loc == nil {
		return
	}
	c := cleanComment(loc.GetLeadingComments())
	if c == "" {
		return
	}
	b.WriteString(c)
	b.WriteString("\n\n")
}

func (si *sourceInfo) commentAt(path string) string {
	loc := si.locs[path]
	if loc == nil {
		return ""
	}
	return cleanComment(loc.GetLeadingComments())
}

// proto field numbers for FileDescriptorProto
const (
	fieldService  = 6
	fieldMessage  = 4
	fieldEnum     = 5
	fieldMethod   = 2 // within ServiceDescriptorProto
	fieldField    = 2 // within DescriptorProto (field)
	fieldNestedMsg = 3 // within DescriptorProto (nested_type)
	fieldNestedEnum = 4 // within DescriptorProto (enum_type)
	fieldOneof    = 8 // within DescriptorProto
	fieldEnumVal  = 2 // within EnumDescriptorProto
)
