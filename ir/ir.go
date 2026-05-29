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

// Package ir defines the intermediate representation (IR) produced by the
// libopenapi model walker and consumed by the emitters.
//
// The IR is the single contract between parser and emitters: emitters must
// never reach into libopenapi types, and the parser must not assume anything
// about the output format.
package ir

// Kind identifies the shape of a NamedType.
type Kind int

const (
	// KindStruct is a Go struct with named fields.
	KindStruct Kind = iota
	// KindEnum is a named string or numeric type with exported constant values.
	KindEnum
	// KindAlias is a simple Go type alias (e.g. type Foo = string or []Bar).
	KindAlias
	// KindUnion is a oneOf/anyOf wrapper (Strategy B: one *T field per variant).
	KindUnion
)

// TypeRef names a Go type expression for use in field types, parameters, and
// operation return types.
type TypeRef struct {
	// Name is the Go type expression, e.g. "string", "int64", "[]MyType",
	// "map[string]Foo", "time.Time", "MyNamedType".
	Name string
	// IsBuiltin is true for built-in Go types that require no import.
	IsBuiltin bool
	// NeedsTime is true when Name contains "time.Time" and the "time" package
	// must be imported.
	NeedsTime bool
	// IsEnum is true when the named type is a KindEnum (string or numeric alias
	// with exported const values). Only meaningful when !IsBuiltin. Used by the
	// params emitter to determine whether string(x) is a valid cast.
	IsEnum bool
	// IsSlice is true when the underlying Go type is a slice, whether expressed
	// inline (Name begins with "[]") or as a named alias to a slice
	// (e.g. type MySlice []T). Used by the client emitter to decide that the
	// type is returned by value rather than by pointer.
	IsSlice bool
	// IsMap is true when the underlying Go type is a map, whether expressed
	// inline (Name begins with "map[") or as a named alias to a map
	// (e.g. type MyMap map[string]T). Used by the client emitter to decide that
	// the type is returned by value rather than by pointer.
	IsMap bool
}

// Field is one field of a generated Go struct.
type Field struct {
	// Name is the exported Go field name.
	Name string
	// JSONName is the original JSON property key, used for the json struct tag.
	JSONName string
	// GoType is the resolved Go type for this field.
	GoType TypeRef
	// IsPointer indicates whether the field type is wrapped in a pointer, per
	// the optional/non-nilable rule: pointer iff non-required and type is
	// non-nilable (bool, numeric, time.Time, named struct/union).
	IsPointer bool
	// IsRequired records whether the OpenAPI schema marks this property required.
	IsRequired bool
	// IsEmbedded is true for anonymous embedded fields (allOf struct embedding).
	IsEmbedded bool
	// DocComment is the field-level godoc comment text (without leading "//").
	DocComment string
	// OmitZero is always true per project style (json:",omitzero" tag).
	OmitZero bool
}

// EnumValue is one exported constant in a generated enum const block.
type EnumValue struct {
	// Name is the exported Go const name.
	Name string
	// Value is the string or numeric literal assigned to this constant.
	Value string
}

// UnionVariant is one arm of a oneOf/anyOf wrapper struct.
type UnionVariant struct {
	// FieldName is the exported field name in the wrapper struct.
	FieldName string
	// GoType is the type of this variant.
	GoType TypeRef
	// IsPrimitive is true when the variant resolves to a primitive Go type.
	// Such variants trigger a Strategy A fallback (any field) per plan §5.
	IsPrimitive bool
}

// NamedType is one Go type declaration produced by the generator.
type NamedType struct {
	// Name is the exported Go identifier.
	Name string
	// OriginalName is the raw OpenAPI schema name preserved for godoc.
	OriginalName string
	// Kind identifies the shape of this type.
	Kind Kind
	// DocComment is the godoc comment text (without leading "//").
	// Must end with a period per project style.
	DocComment string
	// Fields is populated for KindStruct.
	Fields []Field
	// EnumValues is populated for KindEnum.
	EnumValues []EnumValue
	// AliasTarget holds the underlying type for KindAlias and the underlying
	// scalar type for KindEnum.
	AliasTarget *TypeRef
	// UnionVariants is populated for KindUnion.
	UnionVariants []UnionVariant
	// IsAnyOf distinguishes anyOf (true) from oneOf (false) in godoc only.
	IsAnyOf bool
}

// ParamIn identifies where an operation parameter is transmitted.
type ParamIn string

const (
	// ParamInPath is a URI path parameter ({id} style).
	ParamInPath ParamIn = "path"
	// ParamInQuery is a URL query parameter (?key=value style).
	ParamInQuery ParamIn = "query"
	// ParamInHeader is an HTTP request header parameter.
	ParamInHeader ParamIn = "header"
	// ParamInCookie is an HTTP cookie parameter (deferred in v1).
	ParamInCookie ParamIn = "cookie"
)

// Param is a typed parameter for an API operation.
type Param struct {
	// Name is the original OpenAPI parameter name.
	Name string
	// GoName is the exported Go field name.
	GoName string
	// In identifies the parameter location.
	In ParamIn
	// GoType is the resolved Go type.
	GoType TypeRef
	// IsPointer indicates the field is wrapped in a pointer (optional non-nilable).
	IsPointer bool
	// Required records whether the parameter is required per the spec.
	Required bool
	// DocComment is the parameter-level godoc text.
	DocComment string
}

// Operation is one API operation ready for code generation.
type Operation struct {
	// ID is the original operationId string.
	ID string
	// GoName is the exported Go method name.
	GoName string
	// Method is the uppercase HTTP method ("GET", "POST", …).
	Method string
	// Path is the raw OpenAPI path string ("/v1/resources/{id}").
	Path string
	// PathParams are path parameters, in spec order.
	PathParams []Param
	// QueryParams are query parameters, in spec order.
	QueryParams []Param
	// HeaderParams are header parameters, in spec order.
	HeaderParams []Param
	// RequestBody is the resolved JSON request body type, nil when absent.
	RequestBody *TypeRef
	// Responses maps HTTP status codes to their resolved types. A nil *TypeRef
	// means the response has no JSON body.
	Responses map[int]*TypeRef
	// DocComment is the operation-level godoc text.
	DocComment string
}

// IR is the complete intermediate representation built from one OpenAPI document.
type IR struct {
	// PackageName is the derived Go package name for all generated files.
	PackageName string
	// Schemas is the ordered list of named types derived from components/schemas.
	Schemas []NamedType
	// InlineTypes holds anonymous schemas promoted to named types, emitted after
	// Schemas. Ordering matches encounter order (deterministic because iteration
	// over libopenapi orderedmaps uses FromOldest()).
	InlineTypes []NamedType
	// Operations is the ordered list of API operations (path order, then method order).
	Operations []Operation
}

// DiagnosticKind classifies the severity of a Diagnostic.
type DiagnosticKind string

const (
	// DiagnosticWarning is a non-fatal advisory for a deferred or unsupported feature.
	DiagnosticWarning DiagnosticKind = "warning"
	// DiagnosticInfo is an informational message about a generator decision.
	DiagnosticInfo DiagnosticKind = "info"
)

// Diagnostic is a non-fatal message produced by the IR builder for features
// that are deferred (SSE, multipart, callbacks, webhooks, external $refs,
// oneOf-of-primitives).
type Diagnostic struct {
	// Kind classifies the severity.
	Kind DiagnosticKind
	// Message is a human-readable explanation.
	Message string
	// Location identifies the schema path or operationId where the issue arose.
	Location string
}
