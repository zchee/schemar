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
	"go/format"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/zchee/schemar/emit"
	"github.com/zchee/schemar/ir"
)

// makeIR builds a minimal IR with one operation for each test scenario.
func makeIR(ops []ir.Operation) *ir.IR {
	return &ir.IR{
		PackageName: "testpkg",
		Operations:  ops,
	}
}

// makeQueryParam constructs an ir.Param for a query parameter test.
func makeQueryParam(name, goType string, isPointer bool) ir.Param {
	return ir.Param{
		Name:      name,
		GoName:    strings.ToUpper(name[:1]) + name[1:],
		In:        ir.ParamInQuery,
		GoType:    ir.TypeRef{Name: goType},
		IsPointer: isPointer,
	}
}

// makeHeaderParam constructs an ir.Param for a header parameter test.
// The GoName is derived by stripping hyphens so it is a valid Go identifier.
func makeHeaderParam(name, goType string) ir.Param {
	// Remove hyphens and capitalise each segment to form a valid Go identifier.
	// e.g. "X-Request-Id" → "XRequestId".
	parts := strings.Split(name, "-")
	var goName strings.Builder
	for _, p := range parts {
		if p != "" {
			goName.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return ir.Param{
		Name:   name,
		GoName: goName.String(),
		In:     ir.ParamInHeader,
		GoType: ir.TypeRef{Name: goType},
	}
}

// containsAll asserts all substrings appear in s.
func containsAll(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("expected output to contain %q\nfull output:\n%s", sub, s)
		}
	}
}

// TestParams_Empty ensures no struct is emitted when there are no query/header params.
func TestParams_Empty(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{
		{GoName: "GetWidget", Method: "GET", Path: "/widgets", Responses: map[int]*ir.TypeRef{}},
	})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	if strings.Contains(string(out), "type ") {
		t.Errorf("expected no struct declarations; got:\n%s", out)
	}
}

// TestParams_StringField verifies string query parameter encoding with form tag.
func TestParams_StringField(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{{
		GoName:      "ListWidgets",
		Method:      "GET",
		Path:        "/widgets",
		QueryParams: []ir.Param{makeQueryParam("name", "string", false)},
		Responses:   map[int]*ir.TypeRef{},
	}})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		`type ListWidgetsParams struct`,
		`form:"name"`,
		`func (p *ListWidgetsParams) encode() url.Values`,
		`q.Set("name"`,
		`p.Name != ""`,
	)
	if strings.Contains(src, "strconv") {
		t.Error("strconv must not appear for a string-only params struct")
	}
}

// TestParams_BoolField verifies bool query parameter encoding.
func TestParams_BoolField(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{{
		GoName:      "ListWidgets",
		Method:      "GET",
		Path:        "/widgets",
		QueryParams: []ir.Param{makeQueryParam("verbose", "bool", false)},
		Responses:   map[int]*ir.TypeRef{},
	}})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		`strconv.FormatBool(p.Verbose)`,
		`"strconv"`,
	)
}

// TestParams_Int64Field verifies int64 query parameter encoding.
func TestParams_Int64Field(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{{
		GoName:      "ListWidgets",
		Method:      "GET",
		Path:        "/widgets",
		QueryParams: []ir.Param{makeQueryParam("limit", "int64", false)},
		Responses:   map[int]*ir.TypeRef{},
	}})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	containsAll(t, string(out), `strconv.FormatInt(p.Limit, 10)`)
}

// TestParams_PointerField verifies optional (pointer) field nil-guard encoding.
func TestParams_PointerField(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{{
		GoName:      "ListWidgets",
		Method:      "GET",
		Path:        "/widgets",
		QueryParams: []ir.Param{makeQueryParam("limit", "int64", true)},
		Responses:   map[int]*ir.TypeRef{},
	}})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		`Limit *int64`,
		`if p.Limit != nil`,
		`strconv.FormatInt(*p.Limit, 10)`,
	)
}

// TestParams_BoolPointer verifies optional bool field encoding.
func TestParams_BoolPointer(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{{
		GoName:      "ListWidgets",
		Method:      "GET",
		Path:        "/widgets",
		QueryParams: []ir.Param{makeQueryParam("enabled", "bool", true)},
		Responses:   map[int]*ir.TypeRef{},
	}})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		`Enabled *bool`,
		`if p.Enabled != nil`,
		`strconv.FormatBool(*p.Enabled)`,
	)
}

// TestParams_SliceField verifies slice (repeated) query parameter encoding.
func TestParams_SliceField(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{{
		GoName:      "ListWidgets",
		Method:      "GET",
		Path:        "/widgets",
		QueryParams: []ir.Param{makeQueryParam("tags", "[]string", false)},
		Responses:   map[int]*ir.TypeRef{},
	}})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		`Tags []string`,
		`form:"tags"`,
		`for _, v := range p.Tags`,
		`q.Add("tags", v)`,
	)
}

// TestParams_TimeField verifies time.Time query parameter encoding.
func TestParams_TimeField(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{{
		GoName:      "ListWidgets",
		Method:      "GET",
		Path:        "/widgets",
		QueryParams: []ir.Param{makeQueryParam("since", "time.Time", false)},
		Responses:   map[int]*ir.TypeRef{},
	}})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		`Since time.Time`,
		`"time"`,
		`.Format(time.RFC3339)`,
	)
}

// TestParams_HeaderField verifies header parameter encoding via encodeHeaders().
func TestParams_HeaderField(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{{
		GoName:       "GetWidget",
		Method:       "GET",
		Path:         "/widgets/{id}",
		QueryParams:  []ir.Param{makeQueryParam("q", "string", false)},
		HeaderParams: []ir.Param{makeHeaderParam("X-Request-Id", "string")},
		Responses:    map[int]*ir.TypeRef{},
	}})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		`header:"X-Request-Id"`,
		`func (p *GetWidgetParams) encodeHeaders() http.Header`,
		`h.Set("X-Request-Id"`,
	)
}

// TestParams_MultipleOps verifies multiple operations each produce their own struct.
func TestParams_MultipleOps(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{
		{
			GoName:      "ListWidgets",
			Method:      "GET",
			Path:        "/widgets",
			QueryParams: []ir.Param{makeQueryParam("name", "string", false)},
			Responses:   map[int]*ir.TypeRef{},
		},
		{
			GoName:      "SearchWidgets",
			Method:      "GET",
			Path:        "/widgets/search",
			QueryParams: []ir.Param{makeQueryParam("q", "string", false)},
			Responses:   map[int]*ir.TypeRef{},
		},
	})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		`type ListWidgetsParams struct`,
		`type SearchWidgetsParams struct`,
	)
}

// TestParams_FloatEncoding verifies 'g' format is used for floats.
func TestParams_FloatEncoding(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{{
		GoName:      "GetWidget",
		Method:      "GET",
		Path:        "/widgets",
		QueryParams: []ir.Param{makeQueryParam("scale", "float64", false)},
		Responses:   map[int]*ir.TypeRef{},
	}})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	// Must use 'g' format, not 'f'.
	containsAll(t, string(out), `'g', -1, 64`)
	if strings.Contains(string(out), `'f'`) {
		t.Error("float should use 'g' format, got 'f'")
	}
}

// TestParams_FormatValid verifies output is idempotently formatted by go/format.
func TestParams_FormatValid(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{{
		GoName: "ComplexOp",
		Method: "GET",
		Path:   "/complex",
		QueryParams: []ir.Param{
			makeQueryParam("name", "string", false),
			makeQueryParam("limit", "int64", true),
			makeQueryParam("verbose", "bool", false),
			makeQueryParam("tags", "[]string", false),
		},
		HeaderParams: []ir.Param{makeHeaderParam("X-Trace-Id", "string")},
		Responses:    map[int]*ir.TypeRef{},
	}})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	reformatted, err := format.Source(out)
	if err != nil {
		t.Fatalf("output is not valid Go: %v\nsource:\n%s", err, out)
	}
	if diff := gocmp.Diff(string(out), string(reformatted)); diff != "" {
		t.Errorf("output not idempotently formatted (-got +reformatted):\n%s", diff)
	}
}

// TestParams_NoReflect asserts the generated source contains no reflect usage.
func TestParams_NoReflect(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{{
		GoName:      "ListWidgets",
		Method:      "GET",
		Path:        "/widgets",
		QueryParams: []ir.Param{makeQueryParam("q", "string", false)},
		Responses:   map[int]*ir.TypeRef{},
	}})
	out, err := emit.Params(irr, "testpkg")
	if err != nil {
		t.Fatalf("Params: %v", err)
	}
	if strings.Contains(string(out), "reflect") {
		t.Error("generated params.go must not import or use reflect")
	}
}
