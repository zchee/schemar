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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/zchee/schemar/emit"
	"github.com/zchee/schemar/ir"
	"github.com/zchee/schemar/spec"
)

// generateConfig holds the flags accepted by the generate subcommand.
type generateConfig struct {
	input   string
	output  string
	pkg     string
	module  string
	verbose bool
	// dryRun is a future hook (post-v1): print file list and byte counts
	// without writing any files. Reserved here so the flag name is stable.
	dryRun bool
}

// Run is the main entry point for the schemar CLI.
// It accepts the argument list (typically [os.Args][1:]) and returns an exit
// code suitable for [os.Exit].
func Run(args []string) int {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return 1
	}

	switch args[0] {
	case "generate":
		if err := runGenerate(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "schemar generate: %v\n", err)
			return 1
		}
		return 0

	case "version":
		runVersion(os.Stdout)
		return 0

	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return 0

	default:
		fmt.Fprintf(os.Stderr, "schemar: unknown subcommand %q\n\n", args[0])
		printUsage(os.Stderr)
		return 2
	}
}

// runVersion writes version information for schemar, libopenapi, and the Go
// runtime to w.
func runVersion(w io.Writer) {
	fmt.Fprintf(w, "schemar    %s\n", Version)
	fmt.Fprintf(w, "libopenapi %s\n", libopenapiVersion())
	fmt.Fprintf(w, "go         %s\n", runtime.Version())
}

// runGenerate parses flags and executes the generate subcommand.
// It returns a non-nil error when validation fails or generation fails.
func runGenerate(args []string) error {
	fs := flag.NewFlagSet("schemar generate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, generateUsage)
		fs.PrintDefaults()
	}

	cfg := &generateConfig{}
	fs.StringVar(&cfg.input, "input", "", "path to OpenAPI spec file (.json or .yaml) — required")
	fs.StringVar(&cfg.output, "output", "", "output `directory` (default: ./<package>/)")
	fs.StringVar(&cfg.pkg, "package", "", "Go package `name` for generated code (default: derived from spec info.title, or \"apiclient\")")
	fs.StringVar(&cfg.module, "module", "", "Go module `path` for generated code")
	fs.BoolVar(&cfg.verbose, "verbose", false, "write progress diagnostics to stderr")
	// Reserved for post-v1: dry-run support.
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "print file list and byte counts without writing (post-v1 hook, currently ignored)")

	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError already printed the error; -help is not a failure.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("parsing generate flags: %w", err)
	}

	if cfg.input == "" {
		fs.Usage()
		return errors.New("--input is required")
	}

	return generate(cfg)
}

// outDirMode is the permission for the generated output directory.
const outDirMode = 0o755

// outFileMode is the permission for generated Go source files.
const outFileMode = 0o644

// emitFile pairs a generated file name with its formatted source bytes.
type emitFile struct {
	name string
	src  []byte
}

// generate executes the full Load → Build → Emit pipeline.
func generate(cfg *generateConfig) error {
	doc, err := spec.Load(cfg.input)
	if err != nil {
		return fmt.Errorf("loading spec %q: %w", cfg.input, err)
	}

	irResult, diags, err := ir.Build(&doc.Model)
	if err != nil {
		return fmt.Errorf("building IR: %w", err)
	}

	// Resolve package name (Option A — single source of truth).
	// Precedence: explicit flag → IR-derived (from info.title) → fallback.
	pkgName := cfg.pkg
	if pkgName == "" {
		pkgName = irResult.PackageName
	}
	if pkgName == "" {
		pkgName = "apiclient"
	}
	irResult.PackageName = pkgName

	outDir := cfg.output
	if outDir == "" {
		outDir = filepath.Join(".", pkgName)
	}
	if err := os.MkdirAll(outDir, outDirMode); err != nil {
		return fmt.Errorf("creating output directory %q: %w", outDir, err)
	}

	// Print IR diagnostics (always on stderr).
	for _, d := range diags {
		fmt.Fprintf(os.Stderr, "schemar: [%s] %s: %s\n", d.Kind, d.Location, d.Message)
	}

	if cfg.verbose {
		specVer := doc.Model.Version
		if specVer == "" {
			specVer = "(unknown)"
		}
		printVerboseStats(specVer, irResult, diags, pkgName, outDir)
	}

	// Filter out component schemas whose Go names are reserved by the other
	// emitters (client.go declares Client and Option; errors.go declares Error).
	filterReservedSchemas(irResult)

	files, err := emitFiles(irResult, pkgName, outDir)
	if err != nil {
		return err
	}
	if err := writeFiles(files, outDir, cfg.verbose); err != nil {
		return err
	}

	runGoVet(outDir, cfg.verbose)
	return nil
}

// printVerboseStats writes spec and IR summary statistics to stderr.
func printVerboseStats(specVer string, irResult *ir.IR, diags []ir.Diagnostic, pkgName, outDir string) {
	fmt.Fprintf(os.Stderr, "schemar: spec version %s\n", specVer)
	fmt.Fprintf(os.Stderr, "schemar: package %q → %s/\n", pkgName, outDir)
	totalParams := 0
	for _, op := range irResult.Operations {
		totalParams += len(op.PathParams) + len(op.QueryParams) + len(op.HeaderParams)
	}
	fmt.Fprintf(os.Stderr, "schemar: %d schemas (%d inline), %d operations, %d parameters\n",
		len(irResult.Schemas), len(irResult.InlineTypes), len(irResult.Operations), totalParams)
	if len(diags) > 0 {
		fmt.Fprintf(os.Stderr, "schemar: %d diagnostic(s) (see above)\n", len(diags))
	}
}

// emitFiles renders every output file for irResult, returning them in write
// order. The params.go and methods.go files are produced only when the spec
// declares at least one operation.
func emitFiles(irResult *ir.IR, pkgName, outDir string) ([]emitFile, error) {
	typesBytes, err := emit.RenderTypes(irResult, filepath.Join(outDir, "types.go.broken"))
	if err != nil {
		return nil, fmt.Errorf("emitting types.go: %w", err)
	}
	clientBytes, err := emit.Client(pkgName)
	if err != nil {
		return nil, fmt.Errorf("emitting client.go: %w", err)
	}
	errorsBytes, err := emit.Errors(pkgName)
	if err != nil {
		return nil, fmt.Errorf("emitting errors.go: %w", err)
	}

	files := []emitFile{
		{name: "types.go", src: typesBytes},
		{name: "client.go", src: clientBytes},
		{name: "errors.go", src: errorsBytes},
	}

	if len(irResult.Operations) == 0 {
		return files, nil
	}

	paramsBytes, err := emit.Params(irResult, pkgName)
	if err != nil {
		return nil, fmt.Errorf("emitting params.go: %w", err)
	}
	methodsBytes, err := emit.Methods(irResult, pkgName)
	if err != nil {
		return nil, fmt.Errorf("emitting methods.go: %w", err)
	}
	return append(
		files,
		emitFile{name: "params.go", src: paramsBytes},
		emitFile{name: "methods.go", src: methodsBytes},
	), nil
}

// writeFiles writes each emitFile into outDir, logging progress when verbose.
//
//nolint:revive // verbose is a logging-output toggle, not a control flag.
func writeFiles(files []emitFile, outDir string, verbose bool) error {
	for _, f := range files {
		outPath := filepath.Join(outDir, f.name)
		if err := os.WriteFile(outPath, f.src, outFileMode); err != nil {
			return fmt.Errorf("writing %s: %w", outPath, err)
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "schemar: wrote %s (%d bytes)\n", outPath, len(f.src))
		}
	}
	return nil
}

// runGoVet runs "go vet" over the generated package as a non-fatal sanity
// check. The hard compile gate lives in the E2E tests.
func runGoVet(outDir string, verbose bool) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, goPath, "vet", "./...")
	cmd.Dir = outDir
	out, err := cmd.CombinedOutput()
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "schemar: go vet warning (output may not compile cleanly):\n%s\n", out)
	case verbose:
		fmt.Fprintln(os.Stderr, "schemar: go vet passed")
	default:
		// go vet succeeded; stay quiet unless verbose.
	}
}

// printUsage writes the top-level usage text to w.
func printUsage(w io.Writer) {
	fmt.Fprintln(w, topLevelUsage)
}

const topLevelUsage = `schemar — OpenAPI 3.x → Go client + types generator

Usage:
  schemar generate --input <path> [flags]
  schemar version
  schemar help

Run "schemar generate -help" for generate flags.`

const generateUsage = `schemar generate — emit Go types and HTTP client from an OpenAPI spec

Usage:
  schemar generate --input <path> [--output <dir>] [--package <name>] [--module <path>] [--verbose]

Flags:`

// reservedGoNames contains the exported type names declared by the fixed
// emitters (client.go, errors.go) that must not be shadowed by schema-derived
// types in types.go. When a component schema maps to one of these names the
// schema entry is silently dropped from the IR before types.go is rendered.
var reservedGoNames = map[string]bool{
	"Client": true, // declared by emit.Client
	"Error":  true, // declared by emit.Errors
	"Option": true, // declared by emit.Client
}

// filterReservedSchemas removes from irResult any NamedType and InlineType
// whose Go name conflicts with a reserved name from another emitter.
// This prevents redeclaration compile errors in the generated package.
func filterReservedSchemas(irResult *ir.IR) {
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
