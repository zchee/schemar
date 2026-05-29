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

	"github.com/zchee/schemar/emit"
)

// TestErrors_PackageName verifies Errors emits the Error type, its methods, and
// the package declaration for the requested package name.
func TestErrors_PackageName(t *testing.T) {
	t.Parallel()
	out, err := emit.Errors("mypkg")
	if err != nil {
		t.Fatalf("Errors: %v", err)
	}
	containsAll(t, string(out),
		"package mypkg",
		"type Error struct",
		"Code    int",
		"Message string",
		"Fixes   []string",
		"Body    []byte",
		"func (e *Error) Error() string",
		"func decodeError(resp *http.Response) *Error",
		"io.ReadAll(",
		"json.Unmarshal(",
	)
}

// TestErrors_CarveOuts verifies the generated errors file omits the pkgsite §10
// carve-outs, such as server-side helpers and an Unwrap method.
func TestErrors_CarveOuts(t *testing.T) {
	t.Parallel()
	out, err := emit.Errors("mypkg")
	if err != nil {
		t.Fatalf("Errors: %v", err)
	}
	src := string(out)
	// §10 carve-outs: no server-side pkgsite helpers, no Unwrap method.
	for _, forbidden := range []string{
		"BadRequest",
		"InternalServerError",
		"Unwrap",
		"derrors",
	} {
		if strings.Contains(src, forbidden) {
			t.Errorf("Errors output must not contain %q (pkgsite §10 carve-out)", forbidden)
		}
	}
}

// TestErrors_JSONTagsUseOmitzero verifies the generated errors file uses omitzero
// JSON struct tags and never omitempty, per project style.
func TestErrors_JSONTagsUseOmitzero(t *testing.T) {
	t.Parallel()
	out, err := emit.Errors("mypkg")
	if err != nil {
		t.Fatalf("Errors: %v", err)
	}
	src := string(out)
	// Project rule: json tags must use omitzero, never omitempty.
	if strings.Contains(src, "omitempty") {
		t.Error("Errors output must not use omitempty; use omitzero per project style")
	}
	containsAll(t, src, "omitzero")
}

// TestErrors_DifferentPackageNames verifies the emitted package declaration
// tracks the package name passed to Errors across several inputs.
func TestErrors_DifferentPackageNames(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		pkgName string
	}{
		"geminiapi": {pkgName: "geminiapi"},
		"openai":    {pkgName: "openai"},
		"apiclient": {pkgName: "apiclient"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := emit.Errors(tc.pkgName)
			if err != nil {
				t.Fatalf("Errors(%q): %v", tc.pkgName, err)
			}
			if !strings.Contains(string(out), "package "+tc.pkgName) {
				t.Errorf("Errors(%q) output does not contain expected package declaration", tc.pkgName)
			}
		})
	}
}
