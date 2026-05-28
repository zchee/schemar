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
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	gocmp "github.com/google/go-cmp/cmp"
	yamlv4 "go.yaml.in/yaml/v4"

	highbase "github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"

	"github.com/zchee/schemar/emit"
	"github.com/zchee/schemar/ir"
	"github.com/zchee/schemar/spec"
)

// update is set with -update to regenerate golden files instead of diffing.
var update = flag.Bool("update", false, "update golden files instead of diffing")

// Spec paths relative to the emit/ package directory.
const (
	googleSpecPath  = "../testdata/google/generativelanguage.googleapis.com/v1beta/interactions/interactions.openapi.yaml"
	openaiSpecPath  = "../testdata/openai/openapi.yaml"
	googleGoldenDir = "../testdata/golden/google"
	openaiGoldenDir = "../testdata/golden/openai"
)

// exampleCase captures one example value extracted from the OpenAPI spec for
// JSON roundtrip testing.
type exampleCase struct {
	location string
	value    any
}

// reservedGoNames mirrors generate()'s reservedGoNames to filter conflicting
// schemas before golden emission.
var reservedGoNames = map[string]bool{
	"Client": true,
	"Error":  true,
	"Option": true,
}

// ── Golden tests ──────────────────────────────────────────────────────────

// TestGolden_Google runs Load→Build→Emit on the Google Gemini interactions
// spec and diffs each output file against testdata/golden/google/.
// Run with -update to regenerate the golden files.
func TestGolden_Google(t *testing.T) {
	t.Parallel()
	runGolden(t, googleSpecPath, googleGoldenDir)
}

// TestGolden_OpenAI runs Load→Build→Emit on the OpenAI spec and diffs each
// output file against testdata/golden/openai/.
// Run with -update to regenerate the golden files.
func TestGolden_OpenAI(t *testing.T) {
	t.Parallel()
	runGolden(t, openaiSpecPath, openaiGoldenDir)
}

// runGolden executes the full generation pipeline on specPath and either
// writes the output to goldenDir (-update mode) or diffs against it.
func runGolden(t *testing.T, specPath, goldenDir string) {
	t.Helper()

	// ── Load ────────────────────────────────────────────────────────────────
	doc, err := spec.Load(specPath)
	if err != nil {
		t.Fatalf("spec.Load(%q): %v", specPath, err)
	}

	// ── Build IR ────────────────────────────────────────────────────────────
	irResult, diags, err := ir.Build(&doc.Model)
	if err != nil {
		t.Fatalf("ir.Build: %v", err)
	}

	// Apply the same schema-filtering that generate() applies before emitting.
	filterReservedForGolden(irResult)

	pkgName := irResult.PackageName
	if pkgName == "" {
		pkgName = "apiclient"
	}
	irResult.PackageName = pkgName

	// ── Emit ─────────────────────────────────────────────────────────────────
	outputs := make(map[string][]byte)

	typesOut, err := emit.RenderTypes(irResult, "")
	if err != nil {
		t.Fatalf("RenderTypes: %v", err)
	}
	outputs["types.go"] = typesOut

	clientOut, err := emit.Client(pkgName)
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	outputs["client.go"] = clientOut

	errorsOut, err := emit.Errors(pkgName)
	if err != nil {
		t.Fatalf("Errors: %v", err)
	}
	outputs["errors.go"] = errorsOut

	if len(irResult.Operations) > 0 {
		paramsOut, err := emit.Params(irResult, pkgName)
		if err != nil {
			t.Fatalf("Params: %v", err)
		}
		outputs["params.go"] = paramsOut

		methodsOut, err := emit.Methods(irResult, pkgName)
		if err != nil {
			t.Fatalf("Methods: %v", err)
		}
		outputs["methods.go"] = methodsOut
	}

	outputs["diagnostics.txt"] = []byte(formatDiagnostics(diags))

	// ── Update or diff ───────────────────────────────────────────────────────
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", goldenDir, err)
		}
		for name, data := range outputs {
			path := filepath.Join(goldenDir, name)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
		}
		t.Logf("updated %d golden files in %s", len(outputs), goldenDir)
		return
	}

	// Sorted iteration so failures are deterministic.
	names := make([]string, 0, len(outputs))
	for n := range outputs {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		got := outputs[name]
		goldenPath := filepath.Join(goldenDir, name)
		want, err := os.ReadFile(goldenPath)
		if os.IsNotExist(err) {
			t.Errorf("golden file missing: %s\n  hint: run `go test ./emit -run TestGolden -update`", goldenPath)
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", goldenPath, err)
		}
		if diff := gocmp.Diff(string(want), string(got)); diff != "" {
			t.Errorf("golden mismatch for %s (-want +got):\n%s\n  hint: run `go test ./emit -run TestGolden -update`", name, diff)
		}
	}
}

// ── JSON roundtrip ────────────────────────────────────────────────────────

// TestJSONRoundtrip_Google verifies that every examples: block in the Google
// spec produces stable, canonicalisable JSON. The test does NOT use the
// generated typed structs (those are compiled in the E2E gate); instead it
// round-trips through map[string]any to verify structural JSON integrity.
func TestJSONRoundtrip_Google(t *testing.T) {
	t.Parallel()

	doc, err := spec.Load(googleSpecPath)
	if err != nil {
		t.Fatalf("spec.Load: %v", err)
	}

	var cases []exampleCase

	// Walk all paths → operations → request + response examples.
	if doc.Model.Paths != nil && doc.Model.Paths.PathItems != nil {
		for pathStr, pi := range doc.Model.Paths.PathItems.FromOldest() {
			for _, pair := range collectPathOps(pi) {
				op := pair.op
				if op == nil {
					continue
				}

				// Request body examples.
				if op.RequestBody != nil && op.RequestBody.Content != nil {
					mt := op.RequestBody.Content.GetOrZero("application/json")
					if mt != nil {
						collectExamples(t, fmt.Sprintf("%s %s requestBody", pair.method, pathStr), mt, &cases)
					}
				}

				// Response examples.
				if op.Responses == nil || op.Responses.Codes == nil {
					continue
				}
				for code, resp := range op.Responses.Codes.FromOldest() {
					if resp == nil || resp.Content == nil {
						continue
					}
					mt := resp.Content.GetOrZero("application/json")
					if mt == nil {
						continue
					}
					loc := fmt.Sprintf("%s %s %s", pair.method, pathStr, code)
					collectExamples(t, loc, mt, &cases)
				}
			}
		}
	}

	if len(cases) == 0 {
		t.Skip("no examples found in Google spec")
	}

	t.Logf("found %d example entries in Google spec", len(cases))

	tests := map[string]struct {
		location string
		value    any
	}{}
	for _, c := range cases {
		tests[c.location] = struct {
			location string
			value    any
		}{c.location, c.value}
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Marshal value to JSON.
			firstJSON, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}

			// Unmarshal JSON into a new value and re-marshal.
			var intermediate any
			if err := json.Unmarshal(firstJSON, &intermediate); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			secondJSON, err := json.Marshal(intermediate)
			if err != nil {
				t.Fatalf("json.Marshal (roundtrip): %v", err)
			}

			// Canonicalize both to normalise key order.
			v1 := jsontext.Value(firstJSON)
			v2 := jsontext.Value(secondJSON)
			if err := v1.Canonicalize(); err != nil {
				t.Fatalf("canonicalize v1: %v", err)
			}
			if err := v2.Canonicalize(); err != nil {
				t.Fatalf("canonicalize v2: %v", err)
			}

			if diff := gocmp.Diff(string(v1), string(v2)); diff != "" {
				t.Errorf("JSON roundtrip mismatch at %s (-original +roundtripped):\n%s", tc.location, diff)
			}
		})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────

// opPairGolden pairs an HTTP method with its libopenapi Operation.
type opPairGolden struct {
	method string
	op     *v3high.Operation
}

// collectPathOps returns the operations for a PathItem in stable order.
func collectPathOps(pi *v3high.PathItem) []opPairGolden {
	var ops []opPairGolden
	if pi.Get != nil {
		ops = append(ops, opPairGolden{"GET", pi.Get})
	}
	if pi.Post != nil {
		ops = append(ops, opPairGolden{"POST", pi.Post})
	}
	if pi.Put != nil {
		ops = append(ops, opPairGolden{"PUT", pi.Put})
	}
	if pi.Patch != nil {
		ops = append(ops, opPairGolden{"PATCH", pi.Patch})
	}
	if pi.Delete != nil {
		ops = append(ops, opPairGolden{"DELETE", pi.Delete})
	}
	return ops
}

// collectExamples appends each named example from a MediaType to cases after
// converting the yaml.Node value to a Go any via YAML round-trip.
func collectExamples(t *testing.T, location string, mt *v3high.MediaType, cases *[]exampleCase) {
	t.Helper()
	if mt.Examples == nil || orderedmap.Len(mt.Examples) == 0 {
		return
	}
	for exName, ex := range mt.Examples.FromOldest() {
		if ex == nil || ex.Value == nil {
			continue
		}
		// Convert *yaml.Node → Go value via YAML marshal/unmarshal.
		yamlBytes, err := yamlv4.Marshal(ex.Value)
		if err != nil {
			t.Logf("yaml.Marshal example %s/%s: %v (skipping)", location, exName, err)
			continue
		}
		var v any
		if err := yamlv4.Unmarshal(yamlBytes, &v); err != nil {
			t.Logf("yaml.Unmarshal example %s/%s: %v (skipping)", location, exName, err)
			continue
		}
		loc := fmt.Sprintf("%s/examples/%s", location, exName)
		*cases = append(*cases, exampleCase{location: loc, value: v})
	}

	// Also handle single MediaType.Example node (alternative to Examples map).
	if mt.Example != nil && orderedmap.Len(mt.Examples) == 0 {
		yamlBytes, err := yamlv4.Marshal(mt.Example)
		if err != nil {
			t.Logf("yaml.Marshal single example at %s: %v (skipping)", location, err)
			return
		}
		var v any
		if err := yamlv4.Unmarshal(yamlBytes, &v); err != nil {
			t.Logf("yaml.Unmarshal single example at %s: %v (skipping)", location, err)
			return
		}
		*cases = append(*cases, exampleCase{location: location + "/example", value: v})
	}
}

// formatDiagnostics serialises ir.Diagnostic values as a sorted, deterministic
// text block suitable for golden-file comparison.
func formatDiagnostics(diags []ir.Diagnostic) string {
	if len(diags) == 0 {
		return "(no diagnostics)\n"
	}
	lines := make([]string, 0, len(diags))
	for _, d := range diags {
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", d.Kind, d.Location, d.Message))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// filterReservedForGolden removes NamedType entries whose Go names conflict
// with types emitted by client.go and errors.go (Client, Option, Error).
// This mirrors the filterReservedSchemas call in cmd/schemar/run.go.
func filterReservedForGolden(irResult *ir.IR) {
	filtered := irResult.Schemas[:0]
	for _, nt := range irResult.Schemas {
		if !reservedGoNames[nt.Name] {
			filtered = append(filtered, nt)
		}
	}
	irResult.Schemas = filtered

	filteredInline := irResult.InlineTypes[:0]
	for _, nt := range irResult.InlineTypes {
		if !reservedGoNames[nt.Name] {
			filteredInline = append(filteredInline, nt)
		}
	}
	irResult.InlineTypes = filteredInline
}

// Compile-time assertion that highbase is used (avoids unused-import error
// if collectExamples is the only user and the compiler inlines it).
var _ = (*highbase.Example)(nil)
