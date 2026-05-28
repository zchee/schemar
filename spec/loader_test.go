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

package spec_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/zchee/schemar/spec"
)

// minimalSwagger20 is a valid minimal Swagger 2.0 document used to verify
// that Load rejects version-2 specs with a clear error.
const minimalSwagger20 = `swagger: "2.0"
info:
  title: Stub
  version: "1.0"
host: example.com
paths: {}
`

// testdataPath returns the absolute path to a file under the repository-root
// testdata/ directory, relative to this package's source location
// (spec/).
func testdataPath(t *testing.T, elem ...string) string {
	t.Helper()
	// CWD during go test is the package directory (spec/).
	parts := append([]string{"..", "testdata"}, elem...)
	return filepath.Join(parts...)
}

func TestLoad(t *testing.T) {
	t.Parallel()

	// Write Swagger 2.0 fixture to a temp file.
	swaggerFile := filepath.Join(t.TempDir(), "swagger.yaml")
	if err := os.WriteFile(swaggerFile, []byte(minimalSwagger20), 0o600); err != nil {
		t.Fatalf("writing swagger fixture: %v", err)
	}

	// Write a malformed spec fixture.
	malformedFile := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(malformedFile, []byte("not: valid: openapi: garbage: [[["), 0o600); err != nil {
		t.Fatalf("writing malformed fixture: %v", err)
	}

	tests := map[string]struct {
		path           string
		wantTitle      string // non-empty → assert model.Model.Info.Title
		wantErrContain string // non-empty → assert error message contains this substring
	}{
		"success: google openapi yaml": {
			path:      testdataPath(t, "google", "generativelanguage.googleapis.com", "v1beta", "interactions", "interactions.openapi.yaml"),
			wantTitle: "Gemini API",
		},
		"success: google openapi json": {
			path:      testdataPath(t, "google", "generativelanguage.googleapis.com", "v1beta", "interactions", "interactions.openapi.json"),
			wantTitle: "Gemini API",
		},
		"success: openai openapi yaml": {
			path:      testdataPath(t, "openai", "openapi.yaml"),
			wantTitle: "OpenAI API",
		},
		"error: swagger 2.0 rejected": {
			path:           swaggerFile,
			wantErrContain: "Swagger 2.0",
		},
		"error: file not found": {
			path:           "/nonexistent/spec.yaml",
			wantErrContain: "reading",
		},
		"error: malformed spec": {
			path:           malformedFile,
			wantErrContain: "parsing",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			model, err := spec.Load(tc.path)

			if tc.wantErrContain != "" {
				if err == nil {
					t.Fatalf("Load(%q): expected error containing %q, got nil", tc.path, tc.wantErrContain)
				}
				if !strings.Contains(err.Error(), tc.wantErrContain) {
					t.Fatalf("Load(%q): error = %q, want it to contain %q", tc.path, err.Error(), tc.wantErrContain)
				}
				return
			}

			if err != nil {
				t.Fatalf("Load(%q): unexpected error: %v", tc.path, err)
			}
			if model == nil {
				t.Fatalf("Load(%q): returned nil model", tc.path)
			}
			if model.Model.Info == nil {
				t.Fatalf("Load(%q): model.Model.Info is nil", tc.path)
			}

			if tc.wantTitle != "" {
				if diff := gocmp.Diff(tc.wantTitle, model.Model.Info.Title); diff != "" {
					t.Errorf("Load(%q) Info.Title mismatch (-want +got):\n%s", tc.path, diff)
				}
			}
		})
	}
}
