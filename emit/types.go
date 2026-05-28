// Copyright 2026 The schemar Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package emit implements the code generators that convert an IR into Go
// source files.  Each exported Render* function accepts an *ir.IR and returns
// formatted Go source bytes ready for writing to disk.
package emit

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strings"
	"text/template"

	"github.com/zchee/schemar/ir"
	"github.com/zchee/schemar/oneof"
)

//go:embed templates/types.tmpl
var typesTmplSrc string

// typesData is the top-level value passed to the types template.
type typesData struct {
	// PackageName is the Go package declaration name.
	PackageName string
	// Imports is the sorted, formatted list of import lines for the import block.
	Imports []string
	// AllTypes is the complete ordered list of NamedType values to emit
	// (Schemas followed by InlineTypes).
	AllTypes []ir.NamedType
	// HasStrategyBUnion is true when at least one union type uses Strategy B
	// (wrapper struct with one *T per variant).  It controls whether the
	// unexported union helper functions are appended to types.go.
	HasStrategyBUnion bool
}

// RenderTypes generates the content of types.go from irData and returns the
// go/format-formatted Go source.
//
// If go/format.Source fails, the unformatted template output is written to
// brokenPath (when non-empty) so the template bug can be inspected, and the
// format error is returned.
func RenderTypes(irData *ir.IR, brokenPath string) ([]byte, error) {
	funcs := template.FuncMap{
		"typeDecl":         renderTypeDecl,
		"unionHelperDecls": renderUnionHelpers,
	}

	tmpl, err := template.New("types").Funcs(funcs).Parse(typesTmplSrc)
	if err != nil {
		return nil, fmt.Errorf("emit: parsing types template: %w", err)
	}

	data := buildTypesData(irData)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("emit: executing types template: %w", err)
	}

	formatted, fmtErr := format.Source(buf.Bytes())
	if fmtErr != nil {
		if brokenPath != "" {
			_ = os.WriteFile(brokenPath, buf.Bytes(), 0o644)
		}
		return nil, fmt.Errorf("emit: formatting types.go (raw output written to %s): %w", brokenPath, fmtErr)
	}

	return formatted, nil
}

// buildTypesData constructs the typesData from an IR.
func buildTypesData(irData *ir.IR) typesData {
	allTypes := make([]ir.NamedType, 0, len(irData.Schemas)+len(irData.InlineTypes))
	allTypes = append(allTypes, irData.Schemas...)
	allTypes = append(allTypes, irData.InlineTypes...)

	hasStrategyBUnion := false
	for _, nt := range allTypes {
		if nt.Kind == ir.KindUnion && !anyPrimitiveVariant(nt.UnionVariants) {
			hasStrategyBUnion = true
			break
		}
	}

	return typesData{
		PackageName:       irData.PackageName,
		Imports:           computeImports(allTypes, hasStrategyBUnion),
		AllTypes:          allTypes,
		HasStrategyBUnion: hasStrategyBUnion,
	}
}

// anyPrimitiveVariant returns true when any variant in the slice is a primitive,
// which triggers a Strategy A fallback for the whole union.
func anyPrimitiveVariant(variants []ir.UnionVariant) bool {
	for _, v := range variants {
		if v.IsPrimitive {
			return true
		}
	}
	return false
}

// computeImports builds a sorted import-declaration list from the types and
// the union strategy flag.  Standard library imports come first, then
// third-party imports; each group is sorted alphabetically by import path.
func computeImports(types []ir.NamedType, hasStrategyBUnion bool) []string {
	paths := make(map[string]string) // path → alias (empty string = no alias)

	for _, nt := range types {
		switch nt.Kind {
		case ir.KindStruct:
			for _, f := range nt.Fields {
				if f.GoType.NeedsTime {
					paths["time"] = ""
				}
			}
		case ir.KindAlias:
			if nt.AliasTarget != nil && nt.AliasTarget.NeedsTime {
				paths["time"] = ""
			}
		}
	}

	if hasStrategyBUnion {
		paths["github.com/go-json-experiment/json"] = "json"
		paths["github.com/go-json-experiment/json/jsontext"] = ""
	}

	if len(paths) == 0 {
		return nil
	}

	var stdlib, thirdparty []string
	for p := range paths {
		if strings.Contains(p, ".") {
			thirdparty = append(thirdparty, p)
		} else {
			stdlib = append(stdlib, p)
		}
	}
	sort.Strings(stdlib)
	sort.Strings(thirdparty)

	result := make([]string, 0, len(paths))
	for _, p := range stdlib {
		result = append(result, formatImport(p, paths[p]))
	}
	for _, p := range thirdparty {
		result = append(result, formatImport(p, paths[p]))
	}
	return result
}

// formatImport returns the import declaration line for a given path and alias.
func formatImport(path, alias string) string {
	if alias == "" {
		return `"` + path + `"`
	}
	return alias + ` "` + path + `"`
}

// ── Type declaration rendering ─────────────────────────────────────────────

// renderTypeDecl is the FuncMap function that renders a single NamedType as a
// complete Go type declaration (including a trailing newline).
func renderTypeDecl(nt ir.NamedType) (string, error) {
	var b strings.Builder
	switch nt.Kind {
	case ir.KindStruct:
		writeStructDecl(&b, nt)
	case ir.KindEnum:
		writeEnumDecl(&b, nt)
	case ir.KindAlias:
		writeAliasDecl(&b, nt)
	case ir.KindUnion:
		writeUnionDecl(&b, nt)
	default:
		return "", fmt.Errorf("emit: unknown NamedType kind %d for %q", nt.Kind, nt.Name)
	}
	return b.String(), nil
}

// writeComment writes each line of comment (without leading "//") as a
// properly prefixed godoc line to b, indented by indent.
func writeComment(b *strings.Builder, comment, indent string) {
	if comment == "" {
		return
	}
	for line := range strings.SplitSeq(comment, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" {
			fmt.Fprintf(b, "%s//\n", indent)
		} else {
			fmt.Fprintf(b, "%s// %s\n", indent, trimmed)
		}
	}
}

// ── Struct ─────────────────────────────────────────────────────────────────

func writeStructDecl(b *strings.Builder, nt ir.NamedType) {
	writeComment(b, nt.DocComment, "")
	fmt.Fprintf(b, "type %s struct {\n", nt.Name)
	for _, f := range nt.Fields {
		writeFieldDecl(b, f)
	}
	fmt.Fprintf(b, "}\n\n")
}

func writeFieldDecl(b *strings.Builder, f ir.Field) {
	if f.DocComment != "" {
		writeComment(b, f.DocComment, "\t")
	}
	if f.IsEmbedded {
		// allOf embedded struct: no json tag, no pointer.
		fmt.Fprintf(b, "\t%s\n", f.GoType.Name)
		return
	}
	typeName := f.GoType.Name
	if f.IsPointer {
		typeName = "*" + typeName
	}
	// Always use omitzero per project style.
	fmt.Fprintf(b, "\t%s %s `json:\"%s,omitzero\"`\n", f.Name, typeName, f.JSONName)
}

// ── Enum ───────────────────────────────────────────────────────────────────

func writeEnumDecl(b *strings.Builder, nt ir.NamedType) {
	underlying := "string"
	if nt.AliasTarget != nil {
		underlying = nt.AliasTarget.Name
	}

	writeComment(b, nt.DocComment, "")
	fmt.Fprintf(b, "type %s %s\n\n", nt.Name, underlying)

	if len(nt.EnumValues) == 0 {
		return
	}

	// Use quoted literals for string enums, bare literals for numeric.
	isString := underlying == "string"

	fmt.Fprintf(b, "// Enum values for %s.\n", nt.Name)
	fmt.Fprintf(b, "const (\n")
	for _, ev := range nt.EnumValues {
		if isString {
			fmt.Fprintf(b, "\t%s %s = %q\n", ev.Name, nt.Name, ev.Value)
		} else {
			fmt.Fprintf(b, "\t%s %s = %s\n", ev.Name, nt.Name, ev.Value)
		}
	}
	fmt.Fprintf(b, ")\n\n")
}

// ── Alias ──────────────────────────────────────────────────────────────────

func writeAliasDecl(b *strings.Builder, nt ir.NamedType) {
	target := "any"
	if nt.AliasTarget != nil {
		target = nt.AliasTarget.Name
	}
	writeComment(b, nt.DocComment, "")
	fmt.Fprintf(b, "type %s %s\n\n", nt.Name, target)
}

// ── Union ──────────────────────────────────────────────────────────────────

func writeUnionDecl(b *strings.Builder, nt ir.NamedType) {
	if anyPrimitiveVariant(nt.UnionVariants) {
		writeStrategyAUnion(b, nt)
	} else {
		writeStrategyBUnion(b, nt)
	}
}

// writeStrategyAUnion emits a Strategy A union: a struct with a single any
// field and the FIXME diagnostic comment.
func writeStrategyAUnion(b *strings.Builder, nt ir.NamedType) {
	writeComment(b, nt.DocComment, "")
	fmt.Fprintf(b, "%s\n", oneof.StrategyADiagnostic)
	unionKind := "oneOf"
	if nt.IsAnyOf {
		unionKind = "anyOf"
	}
	fmt.Fprintf(b, "type %s struct {\n", nt.Name)
	fmt.Fprintf(b, "\t// Value holds the %s value as an untyped any.\n", unionKind)
	fmt.Fprintf(b, "\tValue any\n")
	fmt.Fprintf(b, "}\n\n")
}

// writeStrategyBUnion emits a Strategy B union: a wrapper struct with one
// *T field per variant, a raw jsontext.Value fallback, and MarshalJSON /
// UnmarshalJSON methods that use the inlined helpers.
func writeStrategyBUnion(b *strings.Builder, nt ir.NamedType) {
	unionKind := "oneOf"
	if nt.IsAnyOf {
		unionKind = "anyOf"
	}

	// Struct declaration.
	writeComment(b, nt.DocComment, "")
	fmt.Fprintf(b, "// It is a %s of:\n", unionKind)
	for _, v := range nt.UnionVariants {
		fmt.Fprintf(b, "//   - %s\n", v.GoType.Name)
	}
	fmt.Fprintf(b, "type %s struct {\n", nt.Name)
	for _, v := range nt.UnionVariants {
		fmt.Fprintf(b, "\t%s *%s\n", v.FieldName, v.GoType.Name)
	}
	fmt.Fprintf(b, "\traw jsontext.Value // retained when no variant matched\n")
	fmt.Fprintf(b, "}\n\n")

	// MarshalJSON.
	fmt.Fprintf(b, "// MarshalJSON implements json.Marshaler for %s.\n", nt.Name)
	fmt.Fprintf(b, "// It encodes whichever variant field is non-nil, or the raw retained bytes.\n")
	fmt.Fprintf(b, "func (u *%s) MarshalJSON() ([]byte, error) {\n", nt.Name)
	fmt.Fprintf(b, "\tswitch {\n")
	for _, v := range nt.UnionVariants {
		fmt.Fprintf(b, "\tcase u.%s != nil:\n\t\treturn json.Marshal(u.%s)\n", v.FieldName, v.FieldName)
	}
	fmt.Fprintf(b, "\tdefault:\n")
	fmt.Fprintf(b, "\t\tif len(u.raw) > 0 {\n\t\t\treturn []byte(u.raw), nil\n\t\t}\n")
	fmt.Fprintf(b, "\t\treturn []byte(\"null\"), nil\n")
	fmt.Fprintf(b, "\t}\n}\n\n")

	// UnmarshalJSON (trial-decode).
	fmt.Fprintf(b, "// UnmarshalJSON implements json.Unmarshaler for %s.\n", nt.Name)
	fmt.Fprintf(b, "// It tries each variant using strict decode; on total failure the raw bytes\n")
	fmt.Fprintf(b, "// are retained for forensic inspection.\n")
	fmt.Fprintf(b, "func (u *%s) UnmarshalJSON(b []byte) error {\n", nt.Name)

	// Declare target variables.
	for _, v := range nt.UnionVariants {
		fmt.Fprintf(b, "\tvar v%s %s\n", v.FieldName, v.GoType.Name)
	}

	// Call unmarshalTrial.
	fmt.Fprintf(b, "\tidx, err := unmarshalTrial(b, []func([]byte) error{\n")
	for _, v := range nt.UnionVariants {
		fmt.Fprintf(b, "\t\tfunc(data []byte) error { return strictUnmarshal(data, &v%s) },\n", v.FieldName)
	}
	fmt.Fprintf(b, "\t})\n")
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn err\n\t}\n")

	// Switch on result.
	fmt.Fprintf(b, "\tswitch idx {\n")
	for i, v := range nt.UnionVariants {
		fmt.Fprintf(b, "\tcase %d:\n\t\tu.%s = &v%s\n", i, v.FieldName, v.FieldName)
	}
	fmt.Fprintf(b, "\tdefault:\n\t\tu.raw = b\n")
	fmt.Fprintf(b, "\t}\n\treturn nil\n}\n\n")
}

// ── Union helper declarations ──────────────────────────────────────────────

// renderUnionHelpers returns the Go source for the unexported helper functions
// inlined at the bottom of types.go when Strategy B unions are present.
// These helpers mirror oneof.UnmarshalTrial and oneof.StrictUnmarshal
// but are unexported package-locals so the generated package has no runtime
// dependency on the schemar module.
func renderUnionHelpers() string {
	return `// ── oneOf/anyOf runtime helpers ─────────────────────────────────────────────
// Inlined by schemar so the generated package requires no runtime dependency
// on the schemar module itself.

// unmarshalTrial attempts each decoder in order and returns the 0-based index
// of the first decoder that succeeds without error, or -1 when all fail.
// A -1 result means the caller should retain the raw JSON bytes.
func unmarshalTrial(data []byte, decoders []func([]byte) error) (int, error) {
	for i, dec := range decoders {
		if err := dec(data); err == nil {
			return i, nil
		}
	}
	return -1, nil
}

// strictUnmarshal unmarshals data into v using RejectUnknownMembers so that
// JSON objects with unknown fields are rejected.  This ensures trial-decode
// correctly identifies the matching variant rather than accepting the first one.
func strictUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v, json.RejectUnknownMembers(true))
}
`
}
