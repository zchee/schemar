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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// moduleRoot returns the repository root (the directory containing go.mod).
// The test source is at cmd/schemar/, so we go up two levels.
func moduleRoot(t *testing.T) string {
	t.Helper()
	// runtime.Caller(0) gives the path of this source file.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is .../cmd/schemar/e2e_test.go
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// runSchemar runs `go run . generate <args...>` from the module root.
// Using `go run` avoids macOS sandbox exec restrictions that prevent
// running binaries built into /tmp directories.
func runSchemar(t *testing.T, root string, args ...string) {
	t.Helper()
	cmdArgs := append([]string{"run", ".", "generate"}, args...)
	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("schemar generate failed: %v\n%s", err, out)
	}
}

// goJSONExpVersion returns the version of github.com/go-json-experiment/json
// that is required by this module, extracted via `go list -m`.
func goJSONExpVersion(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Version}}", "github.com/go-json-experiment/json")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m github.com/go-json-experiment/json: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// writeGoMod writes a minimal go.mod to dir with the given module path and the
// go-json-experiment version that schemar depends on.
func writeGoMod(t *testing.T, dir, modulePath, jsonExpVersion string) {
	t.Helper()
	content := fmt.Sprintf("module %s\n\ngo 1.26\n\nrequire github.com/go-json-experiment/json %s\n",
		modulePath, jsonExpVersion)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}

// buildGeneratedCode runs `go build ./...` and `go vet ./...` in dir, using
// the local module cache so no network access is required.
func buildGeneratedCode(t *testing.T, dir string) {
	t.Helper()

	// Locate the local module cache download directory so we can use it as
	// a file-based proxy. This avoids any network dependency.
	modCacheCmd := exec.Command("go", "env", "GOMODCACHE")
	modCacheOut, err := modCacheCmd.Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	modCache := strings.TrimSpace(string(modCacheOut))
	proxy := "file://" + filepath.Join(modCache, "cache", "download") + ",off"

	baseEnv := append(os.Environ(), "GOPROXY="+proxy, "GONOSUMDB=*", "GOFLAGS=-mod=mod")

	for _, subcmd := range [][]string{
		{"go", "build", "./..."},
		{"go", "vet", "./..."},
	} {
		cmd := exec.Command(subcmd[0], subcmd[1:]...)
		cmd.Dir = dir
		cmd.Env = baseEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s in generated package failed: %v\n%s", strings.Join(subcmd, " "), err, out)
		}
	}
}

// runCanary writes a tiny canary program into outDir/cmd/canary/main.go that
// imports the generated package, constructs a Client, and runs it. This
// catches name-collision bugs and import failures that go build alone may miss.
func runCanary(t *testing.T, outDir, modulePath, pkgName, root string) {
	t.Helper()

	canaryDir := filepath.Join(outDir, "cmd", "canary")
	if err := os.MkdirAll(canaryDir, 0o755); err != nil {
		t.Fatalf("mkdir canary: %v", err)
	}

	// Write a canary main.go that constructs a Client.
	canaryMain := fmt.Sprintf(`package main

import (
	"fmt"
	"log"

	%q
)

func main() {
	c, err := %s.New("https://api.example.com", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("canary: %%T ok\n", c)
}
`, modulePath, pkgName)

	mainPath := filepath.Join(canaryDir, "main.go")
	if err := os.WriteFile(mainPath, []byte(canaryMain), 0o644); err != nil {
		t.Fatalf("write canary main.go: %v", err)
	}

	// Locate the module cache for offline proxy.
	modCacheCmd := exec.Command("go", "env", "GOMODCACHE")
	modCacheOut, err := modCacheCmd.Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	modCache := strings.TrimSpace(string(modCacheOut))
	proxy := "file://" + filepath.Join(modCache, "cache", "download") + ",off"
	env := append(os.Environ(), "GOPROXY="+proxy, "GONOSUMDB=*", "GOFLAGS=-mod=mod")

	// Build (not run) the canary — it makes no network calls so running it
	// in a test environment could be unreliable, but compilation proves the
	// import and type usage are correct.
	cmd := exec.Command("go", "build", "./cmd/canary/")
	cmd.Dir = outDir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("canary build failed: %v\n%s", err, out)
	}

	// Run the canary binary via `go run` (avoids exec permission issues).
	runCmd := exec.Command("go", "run", "./cmd/canary/")
	runCmd.Dir = outDir
	runCmd.Env = env
	if out, err := runCmd.CombinedOutput(); err != nil {
		t.Fatalf("canary run failed: %v\n%s", err, out)
	}
}

// TestE2E_CompileGate is the CRITICAL acceptance gate (plan §2.B):
// it builds the schemar binary, runs it against both testdata specs, and
// verifies the generated code compiles and passes go vet.
func TestE2E_CompileGate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E compile gate in short mode (-short)")
	}

	root := moduleRoot(t)
	jsonExpVer := goJSONExpVersion(t, root)

	tests := map[string]struct {
		specPath string
		pkgName  string
		module   string
		canary   bool // whether to also run the canary program
	}{
		"google yaml": {
			specPath: filepath.Join(root, "testdata", "google", "generativelanguage.googleapis.com", "v1beta", "interactions", "interactions.openapi.yaml"),
			pkgName:  "geminiapi",
			module:   "example.com/geminiapi",
			canary:   true,
		},
		"google json": {
			specPath: filepath.Join(root, "testdata", "google", "generativelanguage.googleapis.com", "v1beta", "interactions", "interactions.openapi.json"),
			pkgName:  "geminiapi",
			module:   "example.com/geminiapi",
			canary:   false, // same spec as yaml; skip duplicate canary
		},
		"openai yaml": {
			specPath: filepath.Join(root, "testdata", "openai", "openapi.yaml"),
			pkgName:  "openaiapi",
			module:   "example.com/openaiapi",
			canary:   true,
		},
		"synthetic collisions": {
			specPath: filepath.Join(root, "testdata", "synthetic", "collisions.yaml"),
			pkgName:  "collisionapi",
			module:   "example.com/collisionapi",
			canary:   true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Don't run JSON and YAML subtests in parallel to avoid file conflicts.
			outDir := t.TempDir()

			// Step 1: generate code via `go run . generate`.
			runSchemar(
				t, root,
				"--input", tc.specPath,
				"--output", outDir,
				"--package", tc.pkgName,
				"--module", tc.module,
			)

			// Step 2: write go.mod into the output directory.
			writeGoMod(t, outDir, tc.module, jsonExpVer)

			// Step 3: go build + go vet must succeed.
			buildGeneratedCode(t, outDir)

			// Step 4: canary program imports the package, constructs a Client,
			// and runs — proving the generated types are usable end-to-end.
			if tc.canary {
				runCanary(t, outDir, tc.module, tc.pkgName, root)
			}
		})
	}
}
