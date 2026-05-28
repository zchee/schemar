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

// Package spec loads and validates OpenAPI 3.x specifications using
// libopenapi's high-level v3 model.
package spec

import (
	"fmt"
	"os"

	"github.com/pb33f/libopenapi"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// swaggerSpecType is the SpecType value that libopenapi assigns to Swagger 2.0 documents.
const swaggerSpecType = "swagger"

// Load reads the OpenAPI 3.x specification at path (JSON or YAML — format is
// detected automatically by libopenapi), validates that it is not a Swagger
// 2.0 document, and returns the built v3 document model ready for IR
// construction.
//
// Non-fatal circular-reference errors produced by BuildV3Model are silently
// discarded when the model itself is non-nil; the caller receives a usable
// model in that case. Fatal errors (nil model) are always returned.
func Load(path string) (*libopenapi.DocumentModel[v3high.Document], error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("spec: reading %q: %w", path, err)
	}

	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("spec: parsing %q: %w", path, err)
	}

	info := doc.GetSpecInfo()
	if info.SpecType == swaggerSpecType {
		return nil, fmt.Errorf(
			"spec: %q is a Swagger 2.0 document (version %s); schemar requires OpenAPI 3.x — upgrade the spec or use a different tool",
			path, info.Version,
		)
	}

	model, buildErr := doc.BuildV3Model()
	if model == nil {
		if buildErr != nil {
			return nil, fmt.Errorf("spec: building v3 model for %q: %w", path, buildErr)
		}
		return nil, fmt.Errorf("spec: BuildV3Model returned a nil model for %q", path)
	}

	// model is non-nil: any remaining buildErr contains only circular-reference
	// warnings that the caller (IR builder) can tolerate. Discard here.
	return model, nil
}
