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

package emit

import (
	_ "embed"
	"fmt"
	"go/format"
	"strings"
	"text/template"
)

//go:embed templates/errors.tmpl
var errorsTmplSrc string

// Errors generates the content of errors.go for the given package name.
// The output is fully static (the Error type and decodeError helper).
// go/format.Source is applied; on failure the raw output is returned alongside
// the error so callers can write a .broken file for debugging.
func Errors(pkgName string) ([]byte, error) {
	tmpl, err := template.New("errors").Parse(errorsTmplSrc)
	if err != nil {
		return nil, fmt.Errorf("emit/errors: parse template: %w", err)
	}

	data := struct{ PackageName string }{PackageName: pkgName}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return nil, fmt.Errorf("emit/errors: execute template: %w", err)
	}

	formatted, fmtErr := format.Source([]byte(sb.String()))
	if fmtErr != nil {
		return []byte(sb.String()), fmt.Errorf("emit/errors: go/format: %w", fmtErr)
	}
	return formatted, nil
}
