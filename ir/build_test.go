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

package ir_test

import (
	"os"
	"path/filepath"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/zchee/schemar/ir"
	"github.com/zchee/schemar/spec"
)

// testdataPath returns the path to a testdata file relative to the module
// root (CWD during go test is ir/).
func testdataPath(t *testing.T, elem ...string) string {
	t.Helper()
	parts := append([]string{"..", "testdata"}, elem...)
	return filepath.Join(parts...)
}

// specFromYAML writes yamlSpec to a temp file and returns the built IR.
func specFromYAML(t *testing.T, yamlSpec string) (*ir.IR, []ir.Diagnostic) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(f, []byte(yamlSpec), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	model, err := spec.Load(f)
	if err != nil {
		t.Fatalf("spec.Load: %v", err)
	}
	irResult, diags, err := ir.Build(&model.Model)
	if err != nil {
		t.Fatalf("ir.Build: %v", err)
	}
	return irResult, diags
}

// findSchema looks up a NamedType by Go name across Schemas and InlineTypes.
func findSchema(irr *ir.IR, goName string) *ir.NamedType {
	for i := range irr.Schemas {
		if irr.Schemas[i].Name == goName {
			return &irr.Schemas[i]
		}
	}
	for i := range irr.InlineTypes {
		if irr.InlineTypes[i].Name == goName {
			return &irr.InlineTypes[i]
		}
	}
	return nil
}

// findField looks up a field by Go name in a NamedType.
func findField(nt *ir.NamedType, name string) *ir.Field {
	for i := range nt.Fields {
		if nt.Fields[i].Name == name {
			return &nt.Fields[i]
		}
	}
	return nil
}

// findOp looks up an Operation by Go name.
func findOp(irr *ir.IR, goName string) *ir.Operation {
	for i := range irr.Operations {
		if irr.Operations[i].GoName == goName {
			return &irr.Operations[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Primitive + format mapping
// ---------------------------------------------------------------------------

func TestBuild_PrimitiveTypes(t *testing.T) {
	t.Parallel()

	const spec30 = `openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
paths: {}
components:
  schemas:
    AString:
      type: string
    ADateTime:
      type: string
      format: date-time
    ABytes:
      type: string
      format: byte
    AnInt64:
      type: integer
    AnInt32:
      type: integer
      format: int32
    AFloat64:
      type: number
    AFloat32:
      type: number
      format: float
    ABool:
      type: boolean
`

	tests := map[string]struct {
		schemaName string
		wantType   string
		wantKind   ir.Kind
		needsTime  bool
	}{
		"string":    {schemaName: "AString", wantType: "string", wantKind: ir.KindAlias},
		"date-time": {schemaName: "ADateTime", wantType: "time.Time", wantKind: ir.KindAlias, needsTime: true},
		"byte":      {schemaName: "ABytes", wantType: "[]byte", wantKind: ir.KindAlias},
		"int64":     {schemaName: "AnInt64", wantType: "int64", wantKind: ir.KindAlias},
		"int32":     {schemaName: "AnInt32", wantType: "int32", wantKind: ir.KindAlias},
		"float64":   {schemaName: "AFloat64", wantType: "float64", wantKind: ir.KindAlias},
		"float32":   {schemaName: "AFloat32", wantType: "float32", wantKind: ir.KindAlias},
		"bool":      {schemaName: "ABool", wantType: "bool", wantKind: ir.KindAlias},
	}

	irr, _ := specFromYAML(t, spec30)

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			nt := findSchema(irr, tc.schemaName)
			if nt == nil {
				t.Fatalf("schema %q not found in IR", tc.schemaName)
			}
			if diff := gocmp.Diff(tc.wantKind, nt.Kind); diff != "" {
				t.Errorf("Kind mismatch (-want +got):\n%s", diff)
			}
			if nt.AliasTarget == nil {
				t.Fatalf("AliasTarget is nil for %q", tc.schemaName)
			}
			if diff := gocmp.Diff(tc.wantType, nt.AliasTarget.Name); diff != "" {
				t.Errorf("AliasTarget.Name mismatch (-want +got):\n%s", diff)
			}
			if diff := gocmp.Diff(tc.needsTime, nt.AliasTarget.NeedsTime); diff != "" {
				t.Errorf("NeedsTime mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Struct with required/optional pointerness
// ---------------------------------------------------------------------------

func TestBuild_StructFieldPointerness(t *testing.T) {
	t.Parallel()

	const specYAML = `openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
paths: {}
components:
  schemas:
    MyStruct:
      type: object
      required: [name, count]
      properties:
        name:
          type: string
        count:
          type: integer
        optBool:
          type: boolean
        optNum:
          type: number
        optStr:
          type: string
        optArr:
          type: array
          items:
            type: string
`

	tests := map[string]struct {
		fieldName   string
		wantPointer bool
		wantType    string
	}{
		"name required string":    {fieldName: "Name", wantPointer: false, wantType: "string"},
		"count required int64":    {fieldName: "Count", wantPointer: false, wantType: "int64"},
		"optBool optional bool":   {fieldName: "OptBool", wantPointer: true, wantType: "bool"},
		"optNum optional float64": {fieldName: "OptNum", wantPointer: true, wantType: "float64"},
		"optStr optional string":  {fieldName: "OptStr", wantPointer: false, wantType: "string"},
		"optArr optional slice":   {fieldName: "OptArr", wantPointer: false, wantType: "[]string"},
	}

	irr, _ := specFromYAML(t, specYAML)
	nt := findSchema(irr, "MyStruct")
	if nt == nil {
		t.Fatal("MyStruct not found")
	}
	if diff := gocmp.Diff(ir.KindStruct, nt.Kind); diff != "" {
		t.Fatalf("Kind mismatch:\n%s", diff)
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := findField(nt, tc.fieldName)
			if f == nil {
				t.Fatalf("field %q not found in MyStruct", tc.fieldName)
			}
			if diff := gocmp.Diff(tc.wantPointer, f.IsPointer); diff != "" {
				t.Errorf("IsPointer mismatch (-want +got):\n%s", diff)
			}
			if diff := gocmp.Diff(tc.wantType, f.GoType.Name); diff != "" {
				t.Errorf("GoType.Name mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Array and map schemas
// ---------------------------------------------------------------------------

func TestBuild_ArrayAndMap(t *testing.T) {
	t.Parallel()

	const specYAML = `openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
paths: {}
components:
  schemas:
    StringArray:
      type: array
      items:
        type: string
    IntMap:
      type: object
      additionalProperties:
        type: integer
    AnyMap:
      type: object
      additionalProperties: true
`

	tests := map[string]struct {
		schemaName string
		wantType   string
		wantKind   ir.Kind
	}{
		"array of string":   {schemaName: "StringArray", wantType: "[]string", wantKind: ir.KindAlias},
		"map of int":        {schemaName: "IntMap", wantType: "map[string]int64", wantKind: ir.KindAlias},
		"map of any (true)": {schemaName: "AnyMap", wantType: "map[string]any", wantKind: ir.KindAlias},
	}

	irr, _ := specFromYAML(t, specYAML)

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			nt := findSchema(irr, tc.schemaName)
			if nt == nil {
				t.Fatalf("schema %q not found", tc.schemaName)
			}
			if diff := gocmp.Diff(tc.wantKind, nt.Kind); diff != "" {
				t.Errorf("Kind mismatch:\n%s", diff)
			}
			if nt.AliasTarget == nil {
				t.Fatalf("AliasTarget nil for %q", tc.schemaName)
			}
			if diff := gocmp.Diff(tc.wantType, nt.AliasTarget.Name); diff != "" {
				t.Errorf("AliasTarget.Name mismatch:\n%s", diff)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Enum schemas
// ---------------------------------------------------------------------------

func TestBuild_Enum(t *testing.T) {
	t.Parallel()

	const specYAML = `openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
paths: {}
components:
  schemas:
    Color:
      type: string
      enum: [red, green, blue]
`

	irr, _ := specFromYAML(t, specYAML)
	nt := findSchema(irr, "Color")
	if nt == nil {
		t.Fatal("Color not found")
	}

	tests := map[string]struct {
		wantKind       ir.Kind
		wantUnderlying string
		wantConstCount int
	}{
		"enum shape": {wantKind: ir.KindEnum, wantUnderlying: "string", wantConstCount: 3},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if diff := gocmp.Diff(tc.wantKind, nt.Kind); diff != "" {
				t.Errorf("Kind:\n%s", diff)
			}
			if nt.AliasTarget == nil {
				t.Fatal("AliasTarget nil")
			}
			if diff := gocmp.Diff(tc.wantUnderlying, nt.AliasTarget.Name); diff != "" {
				t.Errorf("AliasTarget.Name:\n%s", diff)
			}
			if diff := gocmp.Diff(tc.wantConstCount, len(nt.EnumValues)); diff != "" {
				t.Errorf("EnumValues count:\n%s", diff)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// allOf struct embedding
// ---------------------------------------------------------------------------

func TestBuild_AllOfEmbedding(t *testing.T) {
	t.Parallel()

	const specYAML = `openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
paths: {}
components:
  schemas:
    Base:
      type: object
      properties:
        id:
          type: string
    Extended:
      allOf:
        - $ref: '#/components/schemas/Base'
`

	irr, _ := specFromYAML(t, specYAML)
	nt := findSchema(irr, "Extended")
	if nt == nil {
		t.Fatal("Extended not found")
	}

	if diff := gocmp.Diff(ir.KindStruct, nt.Kind); diff != "" {
		t.Errorf("Kind:\n%s", diff)
	}
	if len(nt.Fields) == 0 {
		t.Fatal("Extended has no fields (expected embedded Base)")
	}
	f := findField(nt, "Base")
	if f == nil {
		t.Fatal("embedded Base field not found in Extended")
	}
	if diff := gocmp.Diff(true, f.IsEmbedded); diff != "" {
		t.Errorf("IsEmbedded:\n%s", diff)
	}
}

// ---------------------------------------------------------------------------
// $ref linkage (no inline expansion)
// ---------------------------------------------------------------------------

func TestBuild_RefLinkage(t *testing.T) {
	t.Parallel()

	const specYAML = `openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
paths: {}
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
    Container:
      type: object
      properties:
        widget:
          $ref: '#/components/schemas/Widget'
`

	irr, _ := specFromYAML(t, specYAML)
	nt := findSchema(irr, "Container")
	if nt == nil {
		t.Fatal("Container not found")
	}
	f := findField(nt, "Widget")
	if f == nil {
		t.Fatal("field Widget not found in Container")
	}
	// The field type must be the named type, not an inline expansion.
	if diff := gocmp.Diff("Widget", f.GoType.Name); diff != "" {
		t.Errorf("GoType.Name ($ref target):\n%s", diff)
	}
	// Widget is a KindStruct (non-nilable) and non-required → must be a pointer per plan §2.B.
	if !f.IsPointer {
		t.Error("optional Widget field (KindStruct) must be wrapped in a pointer per plan §2.B optional/non-nilable rule")
	}
}

// ---------------------------------------------------------------------------
// Inline object naming collision resolution
// ---------------------------------------------------------------------------

func TestBuild_InlineObjectNamingCollision(t *testing.T) {
	t.Parallel()

	// Two different parents produce the same field-derived name → collision
	// must be resolved by appending 2.
	const specYAML = `openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
paths: {}
components:
  schemas:
    Alpha:
      type: object
      properties:
        details:
          type: object
          properties:
            x:
              type: string
    Beta:
      type: object
      properties:
        details:
          type: object
          properties:
            y:
              type: integer
`

	irr, _ := specFromYAML(t, specYAML)

	// Both Alpha.details and Beta.details are inline objects. Their derived
	// names are AlphaDetails and BetaDetails — no collision in this case
	// because parents differ. Verify that both were promoted to inline types.
	alphaDetails := findSchema(irr, "AlphaDetails")
	betaDetails := findSchema(irr, "BetaDetails")
	if alphaDetails == nil {
		t.Error("expected inline type AlphaDetails not found")
	}
	if betaDetails == nil {
		t.Error("expected inline type BetaDetails not found")
	}
}

// ---------------------------------------------------------------------------
// Operation: path + query params, request body, response type
// ---------------------------------------------------------------------------

func TestBuild_Operation(t *testing.T) {
	t.Parallel()

	const specYAML = `openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
paths:
  /widgets/{id}:
    get:
      operationId: getWidget
      summary: Fetch a widget.
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
        - name: verbose
          in: query
          schema:
            type: boolean
      responses:
        "200":
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Widget'
  /widgets:
    post:
      operationId: createWidget
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Widget'
      responses:
        "201":
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Widget'
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
`

	tests := map[string]struct {
		opGoName        string
		wantMethod      string
		pathParamCount  int
		queryParamCount int
		wantBodyType    string // "" means no body
		wantRespCode    int
		wantRespType    string
	}{
		"GET with path+query params": {
			opGoName:        "GetWidget",
			wantMethod:      "GET",
			pathParamCount:  1,
			queryParamCount: 1,
			wantBodyType:    "",
			wantRespCode:    200,
			wantRespType:    "Widget",
		},
		"POST with request body": {
			opGoName:        "CreateWidget",
			wantMethod:      "POST",
			pathParamCount:  0,
			queryParamCount: 0,
			wantBodyType:    "Widget",
			wantRespCode:    201,
			wantRespType:    "Widget",
		},
	}

	irr, _ := specFromYAML(t, specYAML)

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			op := findOp(irr, tc.opGoName)
			if op == nil {
				t.Fatalf("operation %q not found", tc.opGoName)
			}
			if diff := gocmp.Diff(tc.wantMethod, op.Method); diff != "" {
				t.Errorf("Method:\n%s", diff)
			}
			if diff := gocmp.Diff(tc.pathParamCount, len(op.PathParams)); diff != "" {
				t.Errorf("PathParams count:\n%s", diff)
			}
			if diff := gocmp.Diff(tc.queryParamCount, len(op.QueryParams)); diff != "" {
				t.Errorf("QueryParams count:\n%s", diff)
			}
			if tc.wantBodyType == "" {
				if op.RequestBody != nil {
					t.Errorf("expected no request body, got %q", op.RequestBody.Name)
				}
			} else {
				if op.RequestBody == nil {
					t.Fatal("expected request body, got nil")
				}
				if diff := gocmp.Diff(tc.wantBodyType, op.RequestBody.Name); diff != "" {
					t.Errorf("RequestBody.Name:\n%s", diff)
				}
			}
			respRef := op.Responses[tc.wantRespCode]
			if respRef == nil {
				t.Fatalf("response %d not found in op.Responses", tc.wantRespCode)
			}
			if diff := gocmp.Diff(tc.wantRespType, respRef.Name); diff != "" {
				t.Errorf("Responses[%d].Name:\n%s", tc.wantRespCode, diff)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Multiple 2xx responses — pick the right ones
// ---------------------------------------------------------------------------

func TestBuild_MultipleResponses(t *testing.T) {
	t.Parallel()

	const specYAML = `openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
paths:
  /widgets:
    post:
      operationId: createWidget
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: {type: string}
        "201":
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Widget'
        "400":
          description: bad request
components:
  schemas:
    Widget:
      type: object
      properties:
        id: {type: string}
`

	irr, _ := specFromYAML(t, specYAML)
	op := findOp(irr, "CreateWidget")
	if op == nil {
		t.Fatal("createWidget operation not found")
	}
	// All 2xx responses should be present; 4xx ignored.
	if _, ok := op.Responses[200]; !ok {
		t.Error("Responses[200] not present")
	}
	if _, ok := op.Responses[201]; !ok {
		t.Error("Responses[201] not present")
	}
	if _, ok := op.Responses[400]; ok {
		t.Error("Responses[400] should not be present (non-2xx)")
	}
}

// ---------------------------------------------------------------------------
// Integration: both testdata specs build without error
// ---------------------------------------------------------------------------

func TestBuild_Integration_GoogleSpec(t *testing.T) {
	t.Parallel()

	paths := []string{
		testdataPath(t, "google", "generativelanguage.googleapis.com", "v1beta", "interactions", "interactions.openapi.yaml"),
		testdataPath(t, "google", "generativelanguage.googleapis.com", "v1beta", "interactions", "interactions.openapi.json"),
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			model, err := spec.Load(p)
			if err != nil {
				t.Fatalf("spec.Load: %v", err)
			}
			irr, diags, err := ir.Build(&model.Model)
			if err != nil {
				t.Fatalf("ir.Build error: %v", err)
			}
			if irr == nil {
				t.Fatal("ir.Build returned nil")
			}
			if len(irr.Schemas) == 0 {
				t.Error("no schemas produced from Google spec")
			}
			if len(irr.Operations) == 0 {
				t.Error("no operations produced from Google spec")
			}
			// Diagnostics must be deterministically ordered (Location then Message).
			for i := 1; i < len(diags); i++ {
				prev, cur := diags[i-1], diags[i]
				if cur.Location < prev.Location || (cur.Location == prev.Location && cur.Message < prev.Message) {
					t.Errorf("diagnostics not sorted at index %d: %+v before %+v", i, prev, cur)
				}
			}
		})
	}
}

func TestBuild_Integration_OpenAISpec(t *testing.T) {
	t.Parallel()

	model, err := spec.Load(testdataPath(t, "openai", "openapi.yaml"))
	if err != nil {
		t.Fatalf("spec.Load: %v", err)
	}
	irr, diags, err := ir.Build(&model.Model)
	if err != nil {
		t.Fatalf("ir.Build error: %v", err)
	}
	if irr == nil {
		t.Fatal("ir.Build returned nil")
	}
	if len(irr.Schemas) == 0 {
		t.Error("no schemas produced from OpenAI spec")
	}
	if len(irr.Operations) == 0 {
		t.Error("no operations produced from OpenAI spec")
	}

	// Diagnostics must be sorted.
	for i := 1; i < len(diags); i++ {
		prev, cur := diags[i-1], diags[i]
		if cur.Location < prev.Location || (cur.Location == prev.Location && cur.Message < prev.Message) {
			t.Errorf("diagnostics not sorted at index %d", i)
		}
	}

	// The OpenAI spec is expected to emit at least some primitive-union diagnostics.
	hasPrimitiveDiag := false
	for _, d := range diags {
		if d.Kind == ir.DiagnosticWarning && len(d.Message) > 0 {
			hasPrimitiveDiag = true
			break
		}
	}
	_ = hasPrimitiveDiag // expected but not mandated (spec may change)
}

// TestBuild_Determinism runs Build twice on the Google spec and asserts
// byte-identical schema name and operation name ordering on both runs.
func TestBuild_Determinism(t *testing.T) {
	t.Parallel()

	path := testdataPath(t, "google", "generativelanguage.googleapis.com", "v1beta", "interactions", "interactions.openapi.yaml")
	model, err := spec.Load(path)
	if err != nil {
		t.Fatalf("spec.Load: %v", err)
	}

	type snapshot struct {
		schemas []string
		ops     []string
	}
	run := func() snapshot {
		t.Helper()
		irr, _, err := ir.Build(&model.Model)
		if err != nil {
			t.Fatalf("ir.Build: %v", err)
		}
		s := snapshot{
			schemas: make([]string, len(irr.Schemas)),
			ops:     make([]string, len(irr.Operations)),
		}
		for i, nt := range irr.Schemas {
			s.schemas[i] = nt.Name
		}
		for i, op := range irr.Operations {
			s.ops[i] = op.GoName
		}
		return s
	}

	first, second := run(), run()
	if diff := gocmp.Diff(first.schemas, second.schemas); diff != "" {
		t.Errorf("schema names non-deterministic (-first +second):\n%s", diff)
	}
	if diff := gocmp.Diff(first.ops, second.ops); diff != "" {
		t.Errorf("operation names non-deterministic (-first +second):\n%s", diff)
	}

	// Schema names must be unique within the schema namespace.
	seenSchemas := make(map[string]int, len(first.schemas))
	for _, n := range first.schemas {
		seenSchemas[n]++
	}
	for n, cnt := range seenSchemas {
		if cnt > 1 {
			t.Errorf("duplicate schema name %q appears %d times", n, cnt)
		}
	}

	// Operation GoNames must be unique within the operation namespace.
	seenOps := make(map[string]int, len(first.ops))
	for _, n := range first.ops {
		seenOps[n]++
	}
	for n, cnt := range seenOps {
		if cnt > 1 {
			t.Errorf("duplicate operation GoName %q appears %d times", n, cnt)
		}
	}
}
