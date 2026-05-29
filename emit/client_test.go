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
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/zchee/schemar/emit"
	"github.com/zchee/schemar/ir"
)

// ── Client() ─────────────────────────────────────────────────────────────────

func TestClient_PackageName(t *testing.T) {
	t.Parallel()
	out, err := emit.Client("myapi")
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		"package myapi",
		"type Client struct",
		"func New(baseURL string",
		"func WithUserAgent(",
		"func WithAPIKey(",
		"func WithBearerToken(",
		"func WithRequestEditor(",
		"func (c *Client) do(",
		"baseURL must not be empty",
	)
}

func TestClient_CarveOuts(t *testing.T) {
	t.Parallel()
	out, err := emit.Client("myapi")
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	src := string(out)
	// §10 carve-outs: no server-side helpers.
	for _, forbidden := range []string{
		"BadRequest",
		"InternalServerError",
		"derrors",
		"ParseParams",
	} {
		if strings.Contains(src, forbidden) {
			t.Errorf("Client output must not contain %q (pkgsite carve-out §10)", forbidden)
		}
	}
}

// ── Methods() ────────────────────────────────────────────────────────────────

func TestMethods_Empty(t *testing.T) {
	t.Parallel()
	out, err := emit.Methods(&ir.IR{PackageName: "mypkg"}, "mypkg")
	if err != nil {
		t.Fatalf("Methods: %v", err)
	}
	src := string(out)
	if strings.Contains(src, "func (c *Client)") {
		t.Errorf("expected no method declarations for empty IR, got: %s", src)
	}
	containsAll(t, src, "package mypkg")
}

func TestMethods_SimpleGET(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{
		{
			ID:         "getWidget",
			GoName:     "GetWidget",
			Method:     "GET",
			Path:       "/widgets",
			DocComment: "GetWidget fetches a widget.",
			Responses:  map[int]*ir.TypeRef{200: {Name: "Widget"}},
		},
	})
	out, err := emit.Methods(irr, "mypkg")
	if err != nil {
		t.Fatalf("Methods: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		"func (c *Client) GetWidget(ctx context.Context) (*Widget, error)",
		`path := "/widgets"`,
		`c.do(ctx, "GET", path, nil, nil, &out)`, //nolint:dupword // asserts generated do() call args; the repeated nil is required
	)
}

func TestMethods_PathParams(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{
		{
			ID:     "getById",
			GoName: "GetById",
			Method: "GET",
			Path:   "/items/{id}",
			PathParams: []ir.Param{
				{Name: "id", GoName: "ID", In: ir.ParamInPath, GoType: ir.TypeRef{Name: "string"}, Required: true},
			},
			Responses: map[int]*ir.TypeRef{200: {Name: "Item"}},
		},
	})
	out, err := emit.Methods(irr, "mypkg")
	if err != nil {
		t.Fatalf("Methods: %v", err)
	}
	src := string(out)
	// Path param GoName "ID" → paramVarName → "id".
	containsAll(
		t, src,
		"func (c *Client) GetById(ctx context.Context, id string) (*Item, error)",
		"strings.NewReplacer(",
		`"{id}", url.PathEscape(id)`,
		`.Replace("/items/{id}")`,
	)
}

func TestMethods_InitialismPathParam(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{
		{
			ID:     "getVersion",
			GoName: "GetVersion",
			Method: "GET",
			Path:   "/{api_version}/resources",
			PathParams: []ir.Param{
				{Name: "api_version", GoName: "APIVersion", In: ir.ParamInPath, GoType: ir.TypeRef{Name: "string"}, Required: true},
			},
			Responses: map[int]*ir.TypeRef{200: {Name: "Resource"}},
		},
	})
	out, err := emit.Methods(irr, "mypkg")
	if err != nil {
		t.Fatalf("Methods: %v", err)
	}
	src := string(out)
	// "APIVersion" → paramVarName → "apiVersion"
	containsAll(
		t, src,
		"func (c *Client) GetVersion(ctx context.Context, apiVersion string) (*Resource, error)",
		`"{api_version}", url.PathEscape(apiVersion)`,
	)
}

func TestMethods_QueryParams(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{
		{
			ID:     "listWidgets",
			GoName: "ListWidgets",
			Method: "GET",
			Path:   "/widgets",
			QueryParams: []ir.Param{
				{Name: "limit", GoName: "Limit", In: ir.ParamInQuery, GoType: ir.TypeRef{Name: "int64"}, IsPointer: true},
			},
			Responses: map[int]*ir.TypeRef{200: {Name: "WidgetList"}},
		},
	})
	out, err := emit.Methods(irr, "mypkg")
	if err != nil {
		t.Fatalf("Methods: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		"func (c *Client) ListWidgets(ctx context.Context, params *ListWidgetsParams) (*WidgetList, error)",
		"var query url.Values",
		"params.encode()",
		`c.do(ctx, "GET", path, query, nil, &out)`,
	)
}

func TestMethods_RequestBody(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{
		{
			ID:          "createWidget",
			GoName:      "CreateWidget",
			Method:      "POST",
			Path:        "/widgets",
			RequestBody: &ir.TypeRef{Name: "CreateWidgetRequest"},
			Responses:   map[int]*ir.TypeRef{201: {Name: "Widget"}},
		},
	})
	out, err := emit.Methods(irr, "mypkg")
	if err != nil {
		t.Fatalf("Methods: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		"func (c *Client) CreateWidget(ctx context.Context, body *CreateWidgetRequest) (*Widget, error)",
		`c.do(ctx, "POST", path, nil, body, &out)`,
	)
}

func TestMethods_NoJSONResponse(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{
		{
			ID:     "deleteWidget",
			GoName: "DeleteWidget",
			Method: "DELETE",
			Path:   "/widgets/{id}",
			PathParams: []ir.Param{
				{Name: "id", GoName: "ID", In: ir.ParamInPath, GoType: ir.TypeRef{Name: "string"}, Required: true},
			},
			Responses: map[int]*ir.TypeRef{204: nil}, // no body
		},
	})
	out, err := emit.Methods(irr, "mypkg")
	if err != nil {
		t.Fatalf("Methods: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		"func (c *Client) DeleteWidget(ctx context.Context, id string) (*http.Response, error)",
		`return c.do(ctx, "DELETE", path, nil, nil, nil)`, //nolint:dupword // asserts generated do() call args; repeated nil is required
	)
}

func TestMethods_AllArgsOperation(t *testing.T) {
	t.Parallel()
	// Operation with path params + query params + body + JSON response.
	irr := makeIR([]ir.Operation{
		{
			ID:     "updateItem",
			GoName: "UpdateItem",
			Method: "PUT",
			Path:   "/{version}/items/{id}",
			PathParams: []ir.Param{
				{Name: "version", GoName: "Version", In: ir.ParamInPath, GoType: ir.TypeRef{Name: "string"}, Required: true},
				{Name: "id", GoName: "ID", In: ir.ParamInPath, GoType: ir.TypeRef{Name: "string"}, Required: true},
			},
			QueryParams: []ir.Param{
				{Name: "dry_run", GoName: "DryRun", In: ir.ParamInQuery, GoType: ir.TypeRef{Name: "bool"}, IsPointer: true},
			},
			RequestBody: &ir.TypeRef{Name: "UpdateItemRequest"},
			Responses:   map[int]*ir.TypeRef{200: {Name: "Item"}},
		},
	})
	out, err := emit.Methods(irr, "mypkg")
	if err != nil {
		t.Fatalf("Methods: %v", err)
	}
	src := string(out)
	containsAll(
		t, src,
		"func (c *Client) UpdateItem(ctx context.Context, version string, id string, params *UpdateItemParams, body *UpdateItemRequest) (*Item, error)",
		`"{version}", url.PathEscape(version)`,
		`"{id}", url.PathEscape(id)`,
		"params.encode()",
		`c.do(ctx, "PUT", path, query, body, &out)`,
	)
}

func TestMethods_DeterministicOnSameIR(t *testing.T) {
	t.Parallel()
	irr := makeIR([]ir.Operation{
		{GoName: "OpA", Method: "GET", Path: "/a", Responses: map[int]*ir.TypeRef{200: {Name: "A"}}},
		{GoName: "OpB", Method: "POST", Path: "/b", RequestBody: &ir.TypeRef{Name: "BReq"}, Responses: map[int]*ir.TypeRef{201: {Name: "B"}}},
	})
	out1, err1 := emit.Methods(irr, "mypkg")
	out2, err2 := emit.Methods(irr, "mypkg")
	if err1 != nil || err2 != nil {
		t.Fatalf("Methods: %v / %v", err1, err2)
	}
	if diff := gocmp.Diff(string(out1), string(out2)); diff != "" {
		t.Errorf("non-deterministic output (-first +second):\n%s", diff)
	}
}
