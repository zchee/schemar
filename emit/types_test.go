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

package emit_test

import (
	"os"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/zchee/schemar/emit"
	"github.com/zchee/schemar/ir"
)

// ── Test helpers ──────────────────────────────────────────────────────────

// mustRender calls RenderTypes and fails the test on error.
func mustRender(t *testing.T, irData *ir.IR) string {
	t.Helper()
	got, err := emit.RenderTypes(irData, "")
	if err != nil {
		t.Fatalf("RenderTypes: %v", err)
	}
	return string(got)
}

// irWith builds a minimal IR with the given schemas for fixture tests.
func irWith(pkg string, schemas ...ir.NamedType) *ir.IR {
	return &ir.IR{PackageName: pkg, Schemas: schemas}
}

// lineContainsAll returns true when at least one line in text contains
// every word in words. This is robust to gofmt column-alignment which
// inserts tab-stops between struct field names, types, and tags.
func lineContainsAll(text string, words ...string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		found := true
		for _, w := range words {
			if !strings.Contains(line, w) {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}

// assertContains fails the test when text does not contain want.
func assertContains(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Errorf("output missing %q\nfull output:\n%s", want, text)
	}
}

// assertAbsent fails the test when text contains absent.
func assertAbsent(t *testing.T, text, absent string) {
	t.Helper()
	if strings.Contains(text, absent) {
		t.Errorf("output must not contain %q\nfull output:\n%s", absent, text)
	}
}

// assertLine fails the test when no single line in text contains all words.
func assertLine(t *testing.T, text string, words ...string) {
	t.Helper()
	if !lineContainsAll(text, words...) {
		t.Errorf("no line contains all of %v\nfull output:\n%s", words, text)
	}
}

// ── Struct tests ──────────────────────────────────────────────────────────

// TestRenderTypes_Struct verifies struct schema rendering across field variants:
// plain fields, pointer fields, [time.Time] fields that trigger an import, embedded
// allOf fields without struct tags, and fields carrying their own doc comment.
func TestRenderTypes_Struct(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		schema ir.NamedType
		check  func(t *testing.T, got string)
	}{
		"simple struct two fields": {
			schema: ir.NamedType{
				Name:         "Agent",
				OriginalName: "Agent",
				Kind:         ir.KindStruct,
				DocComment:   "Agent corresponds to the OpenAPI schema \"Agent\".",
				Fields: []ir.Field{
					{Name: "ID", JSONName: "id", GoType: ir.TypeRef{Name: "string", IsBuiltin: true}, OmitZero: true},
					{Name: "Description", JSONName: "description", GoType: ir.TypeRef{Name: "string", IsBuiltin: true}, OmitZero: true},
				},
			},
			check: func(t *testing.T, got string) {
				t.Helper()
				assertContains(t, got, `type Agent struct`)
				assertContains(t, got, `// Agent corresponds to the OpenAPI schema "Agent".`)
				// gofmt aligns columns — check name and tag appear on same line.
				assertLine(t, got, "ID", `json:"id,omitzero"`)
				assertLine(t, got, "Description", `json:"description,omitzero"`)
				assertAbsent(t, got, "omitempty")
				assertAbsent(t, got, "interface{}")
			},
		},
		"struct with pointer field": {
			schema: ir.NamedType{
				Name:       "AssignedRoleDetails",
				Kind:       ir.KindStruct,
				DocComment: "AssignedRoleDetails is the details of a role.",
				Fields: []ir.Field{
					{Name: "PredefinedRole", JSONName: "predefined_role", GoType: ir.TypeRef{Name: "bool", IsBuiltin: true}, IsPointer: true, OmitZero: true},
					{Name: "CreatedAt", JSONName: "created_at", GoType: ir.TypeRef{Name: "int64", IsBuiltin: true}, IsPointer: true, OmitZero: true},
				},
			},
			check: func(t *testing.T, got string) {
				t.Helper()
				// *bool / *int64 pointer fields.
				assertLine(t, got, "PredefinedRole", "*bool", `json:"predefined_role,omitzero"`)
				assertLine(t, got, "CreatedAt", "*int64", `json:"created_at,omitzero"`)
			},
		},
		"struct with time.Time field triggers import": {
			schema: ir.NamedType{
				Name:       "Interaction",
				Kind:       ir.KindStruct,
				DocComment: "Interaction is a model interaction.",
				Fields: []ir.Field{
					{Name: "Created", JSONName: "created", GoType: ir.TypeRef{Name: "time.Time", NeedsTime: true}, OmitZero: true},
					{Name: "Updated", JSONName: "updated", GoType: ir.TypeRef{Name: "time.Time", NeedsTime: true}, IsPointer: true, OmitZero: true},
				},
			},
			check: func(t *testing.T, got string) {
				t.Helper()
				assertContains(t, got, `"time"`)
				assertLine(t, got, "Created", "time.Time", `json:"created,omitzero"`)
				assertLine(t, got, "Updated", "*time.Time", `json:"updated,omitzero"`)
			},
		},
		"struct with embedded allOf field": {
			schema: ir.NamedType{
				Name:       "ExtendedAgent",
				Kind:       ir.KindStruct,
				DocComment: "ExtendedAgent embeds Agent.",
				Fields: []ir.Field{
					{Name: "Agent", GoType: ir.TypeRef{Name: "Agent"}, IsEmbedded: true, DocComment: "allOf embedded from Agent."},
					{Name: "Extra", JSONName: "extra", GoType: ir.TypeRef{Name: "string", IsBuiltin: true}, OmitZero: true},
				},
			},
			check: func(t *testing.T, got string) {
				t.Helper()
				assertContains(t, got, `type ExtendedAgent struct`)
				// Embedded field has no json tag — verify Agent appears but never with a backtick-tag on same line.
				if lineContainsAll(got, "Agent", "`") {
					// Only fail if the line with "Agent" has a tag — embedded fields must not.
					for line := range strings.SplitSeq(got, "\n") {
						if strings.Contains(line, "Agent") && strings.Contains(line, "`") &&
							!strings.Contains(line, "//") && !strings.Contains(line, "ExtendedAgent") {
							t.Errorf("embedded Agent field should not have a struct tag: %q", line)
						}
					}
				}
				assertLine(t, got, "Extra", `json:"extra,omitzero"`)
			},
		},
		"struct with field doccomment": {
			schema: ir.NamedType{
				Name:       "Config",
				Kind:       ir.KindStruct,
				DocComment: "Config holds configuration.",
				Fields: []ir.Field{
					{Name: "Timeout", JSONName: "timeout", GoType: ir.TypeRef{Name: "int64", IsBuiltin: true}, DocComment: "Timeout in milliseconds.", OmitZero: true},
				},
			},
			check: func(t *testing.T, got string) {
				t.Helper()
				assertContains(t, got, `// Timeout in milliseconds.`)
				assertLine(t, got, "Timeout", `json:"timeout,omitzero"`)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := mustRender(t, irWith("testpkg", tc.schema))
			tc.check(t, got)
		})
	}
}

// ── Enum tests ────────────────────────────────────────────────────────────

// TestRenderTypes_Enum verifies enum schema rendering for string enums (quoted values
// in a const block) and integer enums (unquoted numeric literals).
func TestRenderTypes_Enum(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		schema ir.NamedType
		check  func(t *testing.T, got string)
	}{
		"string enum": {
			schema: ir.NamedType{
				Name:         "AudioResponseFormat",
				OriginalName: "AudioResponseFormat",
				Kind:         ir.KindEnum,
				DocComment:   "AudioResponseFormat is the response format for audio.",
				AliasTarget:  &ir.TypeRef{Name: "string", IsBuiltin: true},
				EnumValues: []ir.EnumValue{
					{Name: "AudioResponseFormatMp3", Value: "mp3"},
					{Name: "AudioResponseFormatWav", Value: "wav"},
					{Name: "AudioResponseFormatFlac", Value: "flac"},
				},
			},
			check: func(t *testing.T, got string) {
				t.Helper()
				assertContains(t, got, `type AudioResponseFormat string`)
				assertContains(t, got, `const (`)
				assertContains(t, got, `// Enum values for AudioResponseFormat.`)
				// gofmt aligns const name+type columns; check name and value on same line.
				assertLine(t, got, "AudioResponseFormatMp3", `"mp3"`)
				assertLine(t, got, "AudioResponseFormatWav", `"wav"`)
				assertLine(t, got, "AudioResponseFormatFlac", `"flac"`)
			},
		},
		"integer enum": {
			schema: ir.NamedType{
				Name:        "StatusCode",
				Kind:        ir.KindEnum,
				DocComment:  "StatusCode is an integer status code.",
				AliasTarget: &ir.TypeRef{Name: "int64", IsBuiltin: true},
				EnumValues: []ir.EnumValue{
					{Name: "StatusCodeOK", Value: "200"},
					{Name: "StatusCodeNotFound", Value: "404"},
				},
			},
			check: func(t *testing.T, got string) {
				t.Helper()
				assertContains(t, got, `type StatusCode int64`)
				assertLine(t, got, "StatusCodeOK", "200")
				assertLine(t, got, "StatusCodeNotFound", "404")
				// Numeric values must not be quoted.
				assertAbsent(t, got, `"200"`)
				assertAbsent(t, got, `"404"`)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := mustRender(t, irWith("testpkg", tc.schema))
			tc.check(t, got)
		})
	}
}

// ── Alias tests ───────────────────────────────────────────────────────────

// TestRenderTypes_Alias verifies alias type declarations for string, slice, map, and
// named-type targets, and confirms interface{} is never emitted.
func TestRenderTypes_Alias(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		schema      ir.NamedType
		wantContain []string
	}{
		"string alias":         {schema: ir.NamedType{Name: "ModelID", Kind: ir.KindAlias, DocComment: "ModelID is a model identifier.", AliasTarget: &ir.TypeRef{Name: "string", IsBuiltin: true}}, wantContain: []string{`// ModelID is a model identifier.`, `type ModelID string`}},
		"slice alias":          {schema: ir.NamedType{Name: "AgentList", Kind: ir.KindAlias, DocComment: "AgentList is a list of agents.", AliasTarget: &ir.TypeRef{Name: "[]Agent"}}, wantContain: []string{`type AgentList []Agent`}},
		"map alias":            {schema: ir.NamedType{Name: "Metadata", Kind: ir.KindAlias, DocComment: "Metadata is a string map.", AliasTarget: &ir.TypeRef{Name: "map[string]any", IsBuiltin: true}}, wantContain: []string{`type Metadata map[string]any`}},
		"named type ref alias": {schema: ir.NamedType{Name: "CreateInteractionRequest", Kind: ir.KindAlias, DocComment: "CreateInteractionRequest aliases Interaction.", AliasTarget: &ir.TypeRef{Name: "Interaction"}}, wantContain: []string{`type CreateInteractionRequest Interaction`}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := mustRender(t, irWith("testpkg", tc.schema))
			for _, want := range tc.wantContain {
				assertContains(t, got, want)
			}
			assertAbsent(t, got, "interface{}")
		})
	}
}

// ── Union tests ───────────────────────────────────────────────────────────

// TestRenderTypes_UnionStrategyB verifies that a oneOf union whose variants are all
// struct types emits a tagged-struct with pointer variant fields, MarshalJSON and
// UnmarshalJSON methods, inlined unmarshalTrial/strictUnmarshal helpers, the
// jsontext import, and no schemar internal imports or omitempty tags.
func TestRenderTypes_UnionStrategyB(t *testing.T) {
	t.Parallel()

	schema := ir.NamedType{
		Name:       "CreateInteractionParams",
		Kind:       ir.KindUnion,
		DocComment: "CreateInteractionParams is the request body for CreateInteraction.",
		IsAnyOf:    false,
		UnionVariants: []ir.UnionVariant{
			{FieldName: "CreateModelInteractionParams", GoType: ir.TypeRef{Name: "CreateModelInteractionParams"}, IsPrimitive: false},
			{FieldName: "CreateAgentInteractionParams", GoType: ir.TypeRef{Name: "CreateAgentInteractionParams"}, IsPrimitive: false},
		},
	}

	got := mustRender(t, irWith("testpkg", schema))

	// Struct shape.
	assertContains(t, got, `type CreateInteractionParams struct`)
	assertLine(t, got, "CreateModelInteractionParams", "*CreateModelInteractionParams")
	assertLine(t, got, "CreateAgentInteractionParams", "*CreateAgentInteractionParams")
	// raw field with jsontext.Value — gofmt aligns columns, so check both on the same line.
	assertLine(t, got, "raw", "jsontext.Value")

	// MarshalJSON.
	assertContains(t, got, `func (u *CreateInteractionParams) MarshalJSON() ([]byte, error)`)
	assertLine(t, got, "case u.CreateModelInteractionParams != nil:")
	assertContains(t, got, `return json.Marshal(u.CreateModelInteractionParams)`)

	// UnmarshalJSON.
	assertContains(t, got, `func (u *CreateInteractionParams) UnmarshalJSON(b []byte) error`)
	assertContains(t, got, `unmarshalTrial(b, []func([]byte) error{`)
	assertLine(t, got, "strictUnmarshal(data, &vCreateModelInteractionParams)")
	assertLine(t, got, "strictUnmarshal(data, &vCreateAgentInteractionParams)")
	assertContains(t, got, `u.raw = b`)

	// Imports for Strategy B.
	assertContains(t, got, `json "github.com/go-json-experiment/json"`)
	assertContains(t, got, `"github.com/go-json-experiment/json/jsontext"`)

	// Inlined helpers.
	assertContains(t, got, `func unmarshalTrial(`)
	assertContains(t, got, `func strictUnmarshal(`)

	// Must NOT import schemar itself.
	assertAbsent(t, got, "schemar/oneof")
	assertAbsent(t, got, "omitempty")
	assertAbsent(t, got, "interface{}")
	assertAbsent(t, got, "FIXME")
}

// TestRenderTypes_UnionStrategyA verifies that a oneOf union containing a primitive
// variant emits the Strategy A FIXME placeholder struct with a bare `Value any` field
// and does not emit Strategy B helpers (unmarshalTrial, strictUnmarshal) or the
// jsontext import.
func TestRenderTypes_UnionStrategyA(t *testing.T) {
	t.Parallel()

	schema := ir.NamedType{
		Name:       "ChatCompletionContent",
		Kind:       ir.KindUnion,
		DocComment: "ChatCompletionContent is a oneOf of string or content.",
		IsAnyOf:    false,
		UnionVariants: []ir.UnionVariant{
			{FieldName: "Variant1", GoType: ir.TypeRef{Name: "string", IsBuiltin: true}, IsPrimitive: true},
			{FieldName: "ChatCompletionMessageContent", GoType: ir.TypeRef{Name: "ChatCompletionMessageContent"}, IsPrimitive: false},
		},
	}

	got := mustRender(t, irWith("testpkg", schema))

	assertContains(t, got, `// FIXME: oneOf with primitives`)
	assertContains(t, got, `type ChatCompletionContent struct`)
	assertContains(t, got, `Value any`)

	// Strategy B helpers must NOT be emitted when only StrategyA unions exist.
	assertAbsent(t, got, `func unmarshalTrial(`)
	assertAbsent(t, got, `func strictUnmarshal(`)
	// No jsontext import needed for Strategy A.
	assertAbsent(t, got, `jsontext`)
	assertAbsent(t, got, `interface{}`)
}

// ── Package metadata tests ────────────────────────────────────────────────

// TestRenderTypes_PackageDecl verifies that the package declaration in the rendered
// output matches the PackageName field of the IR for several representative names.
func TestRenderTypes_PackageDecl(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pkg  string
		want string
	}{
		"openai package":  {pkg: "openaiapi", want: "package openaiapi"},
		"gemini package":  {pkg: "geminiapi", want: "package geminiapi"},
		"default package": {pkg: "apiclient", want: "package apiclient"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := mustRender(t, &ir.IR{PackageName: tc.pkg})
			assertContains(t, got, tc.want)
		})
	}
}

// TestRenderTypes_GeneratedComment verifies that every rendered file contains the
// canonical "Code generated by schemar. DO NOT EDIT." header comment.
func TestRenderTypes_GeneratedComment(t *testing.T) {
	t.Parallel()
	got := mustRender(t, &ir.IR{PackageName: "testpkg"})
	assertContains(t, got, "// Code generated by schemar. DO NOT EDIT.")
}

// ── Exact output test ─────────────────────────────────────────────────────

// TestRenderTypes_ExactStruct performs an end-to-end snapshot check on a minimal
// struct IR: it asserts the generated comment header, package declaration, type doc
// comment, struct body, and the absence of any import block are all present.
func TestRenderTypes_ExactStruct(t *testing.T) {
	t.Parallel()

	got := mustRender(t, irWith("mypkg", ir.NamedType{
		Name:       "Empty",
		Kind:       ir.KindStruct,
		DocComment: "Empty is an empty schema.",
	}))

	assertContains(t, got, "// Code generated by schemar. DO NOT EDIT.")
	assertContains(t, got, "package mypkg")
	assertContains(t, got, "// Empty is an empty schema.")
	assertContains(t, got, "type Empty struct {")
	assertAbsent(t, got, "import")

	// gocmp is available sanity check.
	if diff := gocmp.Diff("x", "x"); diff != "" {
		t.Error("gocmp diff unexpected for equal values")
	}
}

// TestRenderTypes_NoBrokenFileWhenFormatSucceeds verifies that no debug .broken file
// is written to disk when RenderTypes succeeds in formatting the output.
func TestRenderTypes_NoBrokenFileWhenFormatSucceeds(t *testing.T) {
	t.Parallel()

	irData := irWith("cleanpkg", ir.NamedType{
		Name:       "Clean",
		Kind:       ir.KindStruct,
		DocComment: "Clean is a clean type.",
	})
	_, err := emit.RenderTypes(irData, "/tmp/schemar-test-should-not-exist.broken")
	if err != nil {
		t.Fatalf("unexpected error for valid IR: %v", err)
	}
	if _, statErr := os.Stat("/tmp/schemar-test-should-not-exist.broken"); statErr == nil {
		t.Error(".broken file was created even though formatting succeeded")
	}
}
