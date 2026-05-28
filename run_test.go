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

package main_test

import (
	"os"
	"path/filepath"
	"testing"

	schemar "github.com/zchee/schemar"
)

// testdataPath returns the path to a testdata file relative to the module root.
// CWD during go test for this package is cmd/schemar/.
func testdataPath(t *testing.T, elem ...string) string {
	t.Helper()
	parts := append([]string{"..", "..", "testdata"}, elem...)
	return filepath.Join(parts...)
}

// expectedFiles is the set of files that must be present after a successful generate.
var expectedFiles = []string{
	"types.go",
	"client.go",
	"errors.go",
	"params.go",
	"methods.go",
}

func TestRun_Generate_GoogleYAML(t *testing.T) {
	t.Parallel()
	outDir := t.TempDir()
	input := testdataPath(t, "google", "generativelanguage.googleapis.com", "v1beta", "interactions", "interactions.openapi.yaml")

	exitCode := schemar.Run([]string{"generate", "--input", input, "--output", outDir, "--package", "geminiapi"})
	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d; expected 0", exitCode)
	}

	for _, f := range expectedFiles {
		path := filepath.Join(outDir, f)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected output file %q missing: %v", path, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("output file %q is empty", path)
		}
	}
}

func TestRun_Generate_GoogleJSON(t *testing.T) {
	t.Parallel()
	outDir := t.TempDir()
	input := testdataPath(t, "google", "generativelanguage.googleapis.com", "v1beta", "interactions", "interactions.openapi.json")

	exitCode := schemar.Run([]string{"generate", "--input", input, "--output", outDir, "--package", "geminiapi"})
	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d; expected 0", exitCode)
	}

	for _, f := range expectedFiles {
		if _, err := os.Stat(filepath.Join(outDir, f)); err != nil {
			t.Errorf("expected output file %q missing: %v", f, err)
		}
	}
}

func TestRun_MissingInput(t *testing.T) {
	t.Parallel()
	exitCode := schemar.Run([]string{"generate"})
	if exitCode == 0 {
		t.Error("expected non-zero exit when --input is missing, got 0")
	}
}

func TestRun_BadInput(t *testing.T) {
	t.Parallel()
	exitCode := schemar.Run([]string{"generate", "--input", "/nonexistent/spec.yaml", "--output", t.TempDir()})
	if exitCode == 0 {
		t.Error("expected non-zero exit for nonexistent input file, got 0")
	}
}

func TestRun_Version(t *testing.T) {
	t.Parallel()
	exitCode := schemar.Run([]string{"version"})
	if exitCode != 0 {
		t.Errorf("version returned exit code %d; expected 0", exitCode)
	}
}
