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
	"runtime/debug"
)

// Version is the current schemar release version.
const Version = "v0.1.0"

// libopenapiModulePath is the canonical import path of the libopenapi module.
const libopenapiModulePath = "github.com/pb33f/libopenapi"

// libopenapiVersion returns the version string of the embedded libopenapi
// module read from the binary's build info, or "(unknown)" when unavailable.
func libopenapiVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	for _, dep := range info.Deps {
		if dep.Path == libopenapiModulePath {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			return dep.Version
		}
	}
	return "(unknown)"
}
