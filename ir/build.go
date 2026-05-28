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

package ir

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	highbase "github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"

	"github.com/zchee/schemar/naming"
)

// builder accumulates state during a single IR construction pass.
type builder struct {
	ir            *IR
	diags         []Diagnostic
	allTypeNames  map[string]bool // every Go type name allocated so far
	typeKinds     map[string]Kind // kind of each allocated named type
	inlineTypes   []NamedType     // anonymous schemas promoted to named types
	seenOpGoNames map[string]bool // deduplicate operations with the same operationId
}

// opPair pairs an HTTP method string with its libopenapi Operation.
type opPair struct {
	method string
	op     *v3high.Operation
}

// Build converts an OpenAPI v3 document into the schemar IR.
//
// All ordered-map iteration uses FromOldest() to guarantee that output is
// byte-deterministic across repeated invocations on the same input.
func Build(doc *v3high.Document) (*IR, []Diagnostic, error) {
	b := &builder{
		ir:            &IR{},
		allTypeNames:  make(map[string]bool),
		typeKinds:     make(map[string]Kind),
		seenOpGoNames: make(map[string]bool),
	}

	// Derive package name from spec info.
	if doc.Info != nil {
		b.ir.PackageName = naming.GoPackageName(doc.Info.Title)
	}
	if b.ir.PackageName == "" {
		b.ir.PackageName = "apiclient"
	}

	// Pre-pass: register every component schema name and classify its kind so
	// that isNonNilable() works correctly for forward-reference $refs in pass 1.
	if doc.Components != nil && doc.Components.Schemas != nil {
		for name, proxy := range doc.Components.Schemas.FromOldest() {
			goName := naming.GoExported(name)
			b.allTypeNames[goName] = true
			b.typeKinds[goName] = preClassifyKind(proxy)
		}
	}

	// Pass 1: build NamedTypes from components/schemas in spec order.
	if doc.Components != nil && doc.Components.Schemas != nil {
		for name, proxy := range doc.Components.Schemas.FromOldest() {
			nt, err := b.buildTopLevelSchema(name, proxy)
			if err != nil {
				return nil, b.diags, fmt.Errorf("ir: schema %q: %w", name, err)
			}
			b.typeKinds[nt.Name] = nt.Kind // update with the real kind
			b.ir.Schemas = append(b.ir.Schemas, nt)
		}
	}

	// Pass 2: build Operations from paths in spec order.
	if doc.Paths != nil && doc.Paths.PathItems != nil {
		for pathStr, pi := range doc.Paths.PathItems.FromOldest() {
			for _, pair := range pathItemOps(pi) {
				merged := mergeParams(pi.Parameters, pair.op.Parameters)
				op, err := b.buildOperation(pathStr, pair.method, pair.op, merged)
				if err != nil {
					b.diagWarn(fmt.Sprintf("operation %s %s: %v", pair.method, pathStr, err), pathStr)
					continue
				}
				if op != nil {
					b.ir.Operations = append(b.ir.Operations, *op)
				}
			}
		}
	}

	// Emit deferred-feature diagnostics for callbacks and webhooks.
	if doc.Components != nil {
		if orderedmap.Len(doc.Components.Callbacks) > 0 {
			b.diagWarn("callbacks in components are not supported in v1; skipped", "components/callbacks")
		}
		if orderedmap.Len(doc.Components.PathItems) > 0 {
			b.diagWarn("component path items (webhooks) are not supported in v1; skipped", "components/pathItems")
		}
	}

	b.ir.InlineTypes = b.inlineTypes

	// Sort diagnostics for deterministic output.
	sort.Slice(b.diags, func(i, j int) bool {
		if b.diags[i].Location != b.diags[j].Location {
			return b.diags[i].Location < b.diags[j].Location
		}
		return b.diags[i].Message < b.diags[j].Message
	})

	return b.ir, b.diags, nil
}

// preClassifyKind performs a shallow classification of a SchemaProxy so that
// the kind registry is populated before pass 1 iterates schemas. This lets
// isNonNilable() give correct answers for forward references.
func preClassifyKind(proxy *highbase.SchemaProxy) Kind {
	if proxy == nil || proxy.IsReference() {
		return KindAlias
	}
	schema := proxy.Schema()
	if schema == nil {
		return KindAlias
	}
	return classifyKind(schema)
}

// classifyKind maps a resolved Schema to its IR Kind without building the full
// NamedType. It mirrors the dispatch in schemaToNamedType.
func classifyKind(schema *highbase.Schema) Kind {
	if len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 {
		return KindUnion
	}
	if len(schema.Enum) > 0 {
		return KindEnum
	}
	if len(schema.AllOf) > 0 {
		return KindStruct
	}
	pt := schemaType(schema)
	if pt == "object" || (schema.Properties != nil && schema.Properties.Len() > 0) {
		// additionalProperties-only is a map → alias.
		if (schema.Properties == nil || schema.Properties.Len() == 0) && schema.AdditionalProperties != nil {
			return KindAlias
		}
		return KindStruct
	}
	return KindAlias
}

// buildTopLevelSchema creates a NamedType for one components/schemas entry.
func (b *builder) buildTopLevelSchema(name string, proxy *highbase.SchemaProxy) (NamedType, error) {
	goName := naming.GoExported(name)
	docComment := goName + " corresponds to the OpenAPI schema \"" + name + "\"."

	if proxy.IsReference() {
		refName := refToTypeName(proxy.GetReference())
		return NamedType{
			Name:         goName,
			OriginalName: name,
			Kind:         KindAlias,
			DocComment:   docComment,
			AliasTarget:  &TypeRef{Name: naming.GoExported(refName)},
		}, nil
	}

	schema := proxy.Schema()
	if schema == nil {
		b.diagWarn("schema resolved to nil; treating as any", "components/schemas/"+name)
		return NamedType{
			Name:         goName,
			OriginalName: name,
			Kind:         KindAlias,
			DocComment:   docComment,
			AliasTarget:  &TypeRef{Name: "any", IsBuiltin: true},
		}, nil
	}

	return b.schemaToNamedType(goName, name, schema, docComment)
}

// schemaToNamedType converts a resolved Schema into a NamedType. It does not
// handle $ref proxies — callers must resolve those before calling.
func (b *builder) schemaToNamedType(goName, originalName string, schema *highbase.Schema, docComment string) (NamedType, error) {
	// Union: oneOf / anyOf.
	if len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 {
		return b.buildUnionNamedType(goName, originalName, schema, docComment), nil
	}

	// allOf → struct (with embedding or flattened).
	if len(schema.AllOf) > 0 {
		return b.buildAllOfNamedType(goName, originalName, schema, docComment), nil
	}

	// Enum.
	if len(schema.Enum) > 0 {
		return b.buildEnumNamedType(goName, originalName, schema, docComment), nil
	}

	pt := schemaType(schema)

	// Object / struct.
	if pt == "object" || (schema.Properties != nil && schema.Properties.Len() > 0) {
		// additionalProperties only → map alias.
		if (schema.Properties == nil || schema.Properties.Len() == 0) && schema.AdditionalProperties != nil {
			return b.buildMapAliasNamedType(goName, originalName, schema, docComment), nil
		}
		return b.buildStructNamedType(goName, originalName, schema, docComment)
	}

	// Array alias.
	if pt == "array" {
		itemRef := b.itemsTypeRef(schema, goName, "Item")
		return NamedType{
			Name:         goName,
			OriginalName: originalName,
			Kind:         KindAlias,
			DocComment:   docComment,
			AliasTarget:  &TypeRef{Name: "[]" + itemRef.Name, NeedsTime: itemRef.NeedsTime},
		}, nil
	}

	// Primitive alias (string, integer, number, boolean with format variants).
	if ref := primitiveTypeRef(schema); ref.Name != "" {
		return NamedType{
			Name:         goName,
			OriginalName: originalName,
			Kind:         KindAlias,
			DocComment:   docComment,
			AliasTarget:  &ref,
		}, nil
	}

	// Fallback: any.
	b.diagWarn(fmt.Sprintf("unrecognised schema shape (type=%q); treated as any", pt), "components/schemas/"+originalName)
	return NamedType{
		Name:         goName,
		OriginalName: originalName,
		Kind:         KindAlias,
		DocComment:   docComment,
		AliasTarget:  &TypeRef{Name: "any", IsBuiltin: true},
	}, nil
}

// buildStructNamedType builds a KindStruct NamedType from an object schema.
func (b *builder) buildStructNamedType(goName, originalName string, schema *highbase.Schema, docComment string) (NamedType, error) {
	required := requiredSet(schema)
	var fields []Field
	if schema.Properties != nil {
		for jsonName, propProxy := range schema.Properties.FromOldest() {
			fields = append(fields, b.buildField(jsonName, propProxy, required[jsonName], goName))
		}
	}
	return NamedType{
		Name:         goName,
		OriginalName: originalName,
		Kind:         KindStruct,
		DocComment:   docComment,
		Fields:       fields,
	}, nil
}

// buildField converts one properties entry into a Field.
func (b *builder) buildField(jsonName string, proxy *highbase.SchemaProxy, isRequired bool, parentGoName string) Field {
	goFieldName := naming.GoField(jsonName)
	goType := b.schemaToTypeRef(proxy, parentGoName, goFieldName)
	isPointer := !isRequired && b.isNonNilable(goType.Name)

	var docComment string
	if s := proxy.Schema(); s != nil && s.Description != "" {
		docComment = s.Description
	}

	return Field{
		Name:       goFieldName,
		JSONName:   jsonName,
		GoType:     goType,
		IsPointer:  isPointer,
		IsRequired: isRequired,
		DocComment: docComment,
		OmitZero:   true,
	}
}

// buildEnumNamedType builds a KindEnum NamedType.
func (b *builder) buildEnumNamedType(goName, originalName string, schema *highbase.Schema, docComment string) NamedType {
	underlying := enumUnderlyingType(schema)
	var values []EnumValue
	for _, node := range schema.Enum {
		if node == nil || node.Value == "null" || node.Value == "" {
			continue
		}
		constName := goName + naming.GoExported(node.Value)
		values = append(values, EnumValue{Name: constName, Value: node.Value})
	}
	return NamedType{
		Name:         goName,
		OriginalName: originalName,
		Kind:         KindEnum,
		DocComment:   docComment,
		AliasTarget:  &underlying,
		EnumValues:   values,
	}
}

// buildAllOfNamedType builds a KindStruct NamedType for an allOf schema.
// When all allOf components are $refs, Go struct embedding is used; otherwise
// their fields are flattened into one struct.
func (b *builder) buildAllOfNamedType(goName, originalName string, schema *highbase.Schema, docComment string) NamedType {
	allRefs := true
	for _, sub := range schema.AllOf {
		if !sub.IsReference() {
			allRefs = false
			break
		}
	}

	var fields []Field
	if allRefs {
		for _, sub := range schema.AllOf {
			refName := refToTypeName(sub.GetReference())
			goRefName := naming.GoExported(refName)
			fields = append(fields, Field{
				Name:       goRefName,
				JSONName:   "",
				GoType:     TypeRef{Name: goRefName},
				IsEmbedded: true,
				DocComment: "allOf embedded from " + refName + ".",
			})
		}
	} else {
		for _, sub := range schema.AllOf {
			if sub.IsReference() {
				refName := refToTypeName(sub.GetReference())
				goRefName := naming.GoExported(refName)
				fields = append(fields, Field{
					Name:       goRefName,
					JSONName:   "",
					GoType:     TypeRef{Name: goRefName},
					IsEmbedded: true,
					DocComment: "allOf embedded from " + refName + ".",
				})
				continue
			}
			subSchema := sub.Schema()
			if subSchema == nil {
				continue
			}
			req := requiredSet(subSchema)
			if subSchema.Properties != nil {
				for jsonName, propProxy := range subSchema.Properties.FromOldest() {
					fields = append(fields, b.buildField(jsonName, propProxy, req[jsonName], goName))
				}
			}
		}
	}

	return NamedType{
		Name:         goName,
		OriginalName: originalName,
		Kind:         KindStruct,
		DocComment:   docComment + " Built from allOf composition.",
		Fields:       fields,
	}
}

// buildUnionNamedType builds a KindUnion NamedType for oneOf/anyOf schemas.
// Primitive variants cause a Strategy A fallback diagnostic per plan §5.
func (b *builder) buildUnionNamedType(goName, originalName string, schema *highbase.Schema, docComment string) NamedType {
	isAnyOf := len(schema.AnyOf) > 0
	variants := schema.OneOf
	if isAnyOf {
		variants = schema.AnyOf
	}

	hasPrimitive := false
	var unionVariants []UnionVariant
	for i, vProxy := range variants {
		goType := b.schemaToTypeRef(vProxy, goName, "Variant"+strconv.Itoa(i+1))
		isPrim := isPrimitiveType(goType.Name)
		if isPrim {
			hasPrimitive = true
		}
		fieldName := cleanFieldName(goType.Name, i+1)
		unionVariants = append(unionVariants, UnionVariant{
			FieldName:   fieldName,
			GoType:      goType,
			IsPrimitive: isPrim,
		})
	}

	if hasPrimitive {
		b.diagWarn(
			"oneOf/anyOf contains primitive variants; Strategy A fallback (any field) will be used — see plan §5",
			"components/schemas/"+originalName,
		)
	}

	return NamedType{
		Name:          goName,
		OriginalName:  originalName,
		Kind:          KindUnion,
		DocComment:    docComment,
		IsAnyOf:       isAnyOf,
		UnionVariants: unionVariants,
	}
}

// buildMapAliasNamedType builds a KindAlias NamedType for additionalProperties-only schemas.
func (b *builder) buildMapAliasNamedType(goName, originalName string, schema *highbase.Schema, docComment string) NamedType {
	var underlying TypeRef
	if schema.AdditionalProperties.IsA() {
		elemRef := b.schemaToTypeRef(schema.AdditionalProperties.A, goName, "Value")
		underlying = TypeRef{Name: "map[string]" + elemRef.Name, NeedsTime: elemRef.NeedsTime}
	} else {
		underlying = TypeRef{Name: "map[string]any", IsBuiltin: true}
	}
	return NamedType{
		Name:         goName,
		OriginalName: originalName,
		Kind:         KindAlias,
		DocComment:   docComment,
		AliasTarget:  &underlying,
	}
}

// schemaToTypeRef resolves a SchemaProxy to a TypeRef for use in field and
// parameter types. Anonymous object schemas are promoted to named inline types.
func (b *builder) schemaToTypeRef(proxy *highbase.SchemaProxy, parentGoName, fieldGoName string) TypeRef {
	if proxy == nil {
		return TypeRef{Name: "any", IsBuiltin: true}
	}

	// $ref → named reference; do not inline-expand.
	if proxy.IsReference() {
		refName := refToTypeName(proxy.GetReference())
		goName := naming.GoExported(refName)
		kind, known := b.typeKinds[goName]
		return TypeRef{Name: goName, IsEnum: known && kind == KindEnum}
	}

	schema := proxy.Schema()
	if schema == nil {
		b.diagWarn("field schema resolved to nil", parentGoName+"."+fieldGoName)
		return TypeRef{Name: "any", IsBuiltin: true}
	}

	// Union (oneOf/anyOf) → inline named type.
	if len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 {
		return b.allocInlineType(schema, parentGoName, fieldGoName)
	}

	// allOf → inline named type.
	if len(schema.AllOf) > 0 {
		return b.allocInlineType(schema, parentGoName, fieldGoName)
	}

	// Enum → inline named type.
	if len(schema.Enum) > 0 {
		return b.allocInlineType(schema, parentGoName, fieldGoName)
	}

	pt := schemaType(schema)

	// Array.
	if pt == "array" || (schema.Items != nil && schema.Items.IsA()) {
		elemRef := b.itemsTypeRef(schema, parentGoName, fieldGoName)
		return TypeRef{Name: "[]" + elemRef.Name, NeedsTime: elemRef.NeedsTime}
	}

	// Map (additionalProperties only, no explicit properties).
	hasProps := schema.Properties != nil && schema.Properties.Len() > 0
	if !hasProps && schema.AdditionalProperties != nil {
		if schema.AdditionalProperties.IsA() {
			elemRef := b.schemaToTypeRef(schema.AdditionalProperties.A, parentGoName, fieldGoName+"Value")
			return TypeRef{Name: "map[string]" + elemRef.Name, NeedsTime: elemRef.NeedsTime}
		}
		return TypeRef{Name: "map[string]any", IsBuiltin: true}
	}

	// Scalar primitives.
	if ref := primitiveTypeRef(schema); ref.Name != "" {
		return ref
	}

	// Inline object → promote to named type.
	if pt == "object" || hasProps {
		return b.allocInlineType(schema, parentGoName, fieldGoName)
	}

	// Fallback.
	b.diagWarn(fmt.Sprintf("unrecognised field schema type=%q", pt), parentGoName+"."+fieldGoName)
	return TypeRef{Name: "any", IsBuiltin: true}
}

// allocInlineType promotes an anonymous schema to a named NamedType and
// returns a TypeRef pointing to it.
func (b *builder) allocInlineType(schema *highbase.Schema, parentGoName, fieldGoName string) TypeRef {
	inlineName := b.newInlineName(parentGoName + fieldGoName)
	docComment := inlineName + " is an inline schema under " + parentGoName + "." + fieldGoName + "."
	nt, _ := b.schemaToNamedType(inlineName, parentGoName+"."+fieldGoName, schema, docComment)
	b.typeKinds[nt.Name] = nt.Kind
	b.inlineTypes = append(b.inlineTypes, nt)
	return TypeRef{Name: inlineName, IsEnum: nt.Kind == KindEnum}
}

// newInlineName allocates a unique Go type name derived from base, appending
// integer suffixes to resolve collisions (2, 3, …).
func (b *builder) newInlineName(base string) string {
	if !b.allTypeNames[base] {
		b.allTypeNames[base] = true
		return base
	}
	for i := 2; ; i++ {
		candidate := base + strconv.Itoa(i)
		if !b.allTypeNames[candidate] {
			b.allTypeNames[candidate] = true
			return candidate
		}
	}
}

// itemsTypeRef resolves the element type of an array schema.
func (b *builder) itemsTypeRef(schema *highbase.Schema, parentGoName, fieldGoName string) TypeRef {
	if schema.Items != nil && schema.Items.IsA() {
		return b.schemaToTypeRef(schema.Items.A, parentGoName, fieldGoName+"Item")
	}
	return TypeRef{Name: "any", IsBuiltin: true}
}

// buildOperation converts a libopenapi Operation into an ir.Operation.
func (b *builder) buildOperation(pathStr, method string, op *v3high.Operation, mergedParams []*v3high.Parameter) (*Operation, error) {
	if op == nil {
		return nil, nil
	}

	opID := op.OperationId
	if opID == "" {
		opID = strings.ToLower(method) + pathToID(pathStr)
		b.diagWarn("operationId missing; synthesised "+opID, pathStr)
	}

	goName := naming.GoExported(opID)

	// Deduplicate: the OpenAPI spec allows the same operationId on multiple
	// paths (e.g. Google's ?model: / ?agent: path variants). Keep the first
	// occurrence and emit a diagnostic for subsequent ones.
	if b.seenOpGoNames[goName] {
		b.diagWarn(fmt.Sprintf("duplicate operationId %q at %s %s; skipping subsequent occurrence", opID, method, pathStr), pathStr)
		return nil, nil
	}
	b.seenOpGoNames[goName] = true

	docComment := goName + " calls " + method + " " + pathStr + "."
	if op.Summary != "" {
		docComment = strings.TrimRight(op.Summary, ".") + "."
	}

	irOp := &Operation{
		ID:         opID,
		GoName:     goName,
		Method:     method,
		Path:       pathStr,
		Responses:  make(map[int]*TypeRef),
		DocComment: docComment,
	}

	// Classify merged parameters.
	for _, param := range mergedParams {
		if param == nil {
			continue
		}
		switch param.In {
		case "cookie":
			b.diagWarn("cookie parameter skipped (not in v1 scope)", opID)
			continue
		}
		irParam := b.buildParam(param, goName)
		switch ParamIn(param.In) {
		case ParamInPath:
			irOp.PathParams = append(irOp.PathParams, irParam)
		case ParamInQuery:
			irOp.QueryParams = append(irOp.QueryParams, irParam)
		case ParamInHeader:
			irOp.HeaderParams = append(irOp.HeaderParams, irParam)
		}
	}

	// Request body (application/json only; multipart noted as diagnostic).
	if op.RequestBody != nil {
		if ref := b.contentJSONSchemaRef(op.RequestBody.Content, goName+"Body"); ref != nil {
			irOp.RequestBody = ref
		} else if op.RequestBody.Content != nil && op.RequestBody.Content.GetOrZero("multipart/form-data") != nil {
			b.diagWarn("multipart/form-data request body skipped (not in v1 scope)", opID)
		}
	}

	// Responses: collect all 2xx status codes.
	if op.Responses != nil && op.Responses.Codes != nil {
		for code, resp := range op.Responses.Codes.FromOldest() {
			statusCode, err := strconv.Atoi(code)
			if err != nil || statusCode < 200 || statusCode >= 300 {
				continue
			}
			if resp == nil {
				irOp.Responses[statusCode] = nil
				continue
			}
			ref := b.contentJSONSchemaRef(resp.Content, goName+"Response")
			irOp.Responses[statusCode] = ref
		}
		// SSE (text/event-stream) diagnostic.
		if op.Responses.Codes.GetOrZero("200") != nil {
			resp := op.Responses.Codes.GetOrZero("200")
			if resp != nil && resp.Content != nil && resp.Content.GetOrZero("text/event-stream") != nil {
				b.diagWarn("SSE (text/event-stream) response skipped (not in v1 scope)", opID)
			}
		}
	}

	return irOp, nil
}

// buildParam converts a libopenapi Parameter into an ir.Param.
func (b *builder) buildParam(param *v3high.Parameter, opGoName string) Param {
	goName := naming.GoField(param.Name)
	goType := TypeRef{Name: "string", IsBuiltin: true}
	if param.Schema != nil {
		goType = b.schemaToTypeRef(param.Schema, opGoName+"Params", goName)
	}
	required := param.Required != nil && *param.Required
	return Param{
		Name:      param.Name,
		GoName:    goName,
		In:        ParamIn(param.In),
		GoType:    goType,
		IsPointer: !required && b.isNonNilable(goType.Name),
		Required:  required,
	}
}

// contentJSONSchemaRef extracts the SchemaProxy from the application/json
// entry of a content map and returns a TypeRef for it.
// Returns nil when no application/json entry exists or its schema is absent.
func (b *builder) contentJSONSchemaRef(content *orderedmap.Map[string, *v3high.MediaType], nameHint string) *TypeRef {
	if content == nil {
		return nil
	}
	mt := content.GetOrZero("application/json")
	if mt == nil || mt.Schema == nil {
		return nil
	}
	ref := b.schemaToTypeRef(mt.Schema, nameHint, "")
	return &ref
}

// pathItemOps returns the operations defined on a PathItem in a stable,
// deterministic order: GET POST PUT PATCH DELETE HEAD OPTIONS TRACE.
func pathItemOps(pi *v3high.PathItem) []opPair {
	var ops []opPair
	if pi.Get != nil {
		ops = append(ops, opPair{"GET", pi.Get})
	}
	if pi.Post != nil {
		ops = append(ops, opPair{"POST", pi.Post})
	}
	if pi.Put != nil {
		ops = append(ops, opPair{"PUT", pi.Put})
	}
	if pi.Patch != nil {
		ops = append(ops, opPair{"PATCH", pi.Patch})
	}
	if pi.Delete != nil {
		ops = append(ops, opPair{"DELETE", pi.Delete})
	}
	if pi.Head != nil {
		ops = append(ops, opPair{"HEAD", pi.Head})
	}
	if pi.Options != nil {
		ops = append(ops, opPair{"OPTIONS", pi.Options})
	}
	if pi.Trace != nil {
		ops = append(ops, opPair{"TRACE", pi.Trace})
	}
	return ops
}

// mergeParams merges path-item-level parameters with operation-level parameters.
// Operation-level parameters take precedence for the same (name, in) pair,
// matching the OpenAPI specification override rule.
func mergeParams(pathItemParams, opParams []*v3high.Parameter) []*v3high.Parameter {
	// Index operation params by name+in for O(1) lookup.
	overrides := make(map[string]bool, len(opParams))
	for _, p := range opParams {
		if p != nil {
			overrides[p.Name+"|"+p.In] = true
		}
	}
	result := make([]*v3high.Parameter, 0, len(pathItemParams)+len(opParams))
	for _, p := range pathItemParams {
		if p != nil && !overrides[p.Name+"|"+p.In] {
			result = append(result, p)
		}
	}
	result = append(result, opParams...)
	return result
}

// refToTypeName extracts the bare schema name from a $ref string.
// "#/components/schemas/MyType" → "MyType".
func refToTypeName(ref string) string {
	return path.Base(ref)
}

// schemaType returns the first element of schema.Type, or "".
func schemaType(schema *highbase.Schema) string {
	if len(schema.Type) == 0 {
		return ""
	}
	return schema.Type[0]
}

// primitiveTypeRef converts a scalar schema to a TypeRef.
// Returns a zero TypeRef when the schema is not a recognised primitive.
func primitiveTypeRef(schema *highbase.Schema) TypeRef {
	switch schemaType(schema) {
	case "string":
		switch schema.Format {
		case "date-time":
			return TypeRef{Name: "time.Time", NeedsTime: true}
		case "byte":
			return TypeRef{Name: "[]byte", IsBuiltin: true}
		}
		return TypeRef{Name: "string", IsBuiltin: true}
	case "integer":
		if schema.Format == "int32" {
			return TypeRef{Name: "int32", IsBuiltin: true}
		}
		return TypeRef{Name: "int64", IsBuiltin: true}
	case "number":
		if schema.Format == "float" {
			return TypeRef{Name: "float32", IsBuiltin: true}
		}
		return TypeRef{Name: "float64", IsBuiltin: true}
	case "boolean":
		return TypeRef{Name: "bool", IsBuiltin: true}
	}
	return TypeRef{}
}

// enumUnderlyingType returns the TypeRef for the underlying type of an enum.
func enumUnderlyingType(schema *highbase.Schema) TypeRef {
	switch schemaType(schema) {
	case "integer":
		if schema.Format == "int32" {
			return TypeRef{Name: "int32", IsBuiltin: true}
		}
		return TypeRef{Name: "int64", IsBuiltin: true}
	case "number":
		if schema.Format == "float" {
			return TypeRef{Name: "float32", IsBuiltin: true}
		}
		return TypeRef{Name: "float64", IsBuiltin: true}
	}
	return TypeRef{Name: "string", IsBuiltin: true} // default for string and untyped enums
}

// requiredSet builds a set of required property names for O(1) lookup.
func requiredSet(schema *highbase.Schema) map[string]bool {
	m := make(map[string]bool, len(schema.Required))
	for _, r := range schema.Required {
		m[r] = true
	}
	return m
}

// isNonNilable reports whether goTypeName is a non-nilable Go type that needs
// a pointer wrapper when used as an optional field.
// Nilable types (string, any, slices, maps) never need a pointer.
func (b *builder) isNonNilable(goTypeName string) bool {
	switch goTypeName {
	case "bool", "int32", "int64", "float32", "float64", "time.Time":
		return true
	case "string", "any", "":
		return false
	}
	if strings.HasPrefix(goTypeName, "[]") || strings.HasPrefix(goTypeName, "map[") {
		return false
	}
	// Named type: look up actual kind.
	if k, ok := b.typeKinds[goTypeName]; ok {
		return k == KindStruct || k == KindUnion
	}
	// Unknown external $ref — conservative: treat as struct → needs pointer.
	return true
}

// isPrimitiveType returns true for built-in Go scalar types.
func isPrimitiveType(name string) bool {
	switch name {
	case "string", "bool", "int32", "int64", "float32", "float64", "[]byte", "any", "time.Time":
		return true
	}
	return false
}

// cleanFieldName produces a valid exported Go identifier for a union variant
// field, given the Go type name and a 1-based fallback index.
func cleanFieldName(goTypeName string, idx int) string {
	n := goTypeName
	n = strings.TrimPrefix(n, "[]")
	n = strings.TrimPrefix(n, "map[string]")
	// Strip any remaining generics brackets or non-ident characters.
	if strings.ContainsAny(n, "[].* ") || n == "" || !isExportedIdent(n) {
		return "Variant" + strconv.Itoa(idx)
	}
	return n
}

// isExportedIdent returns true when s is a valid exported Go identifier
// (starts with uppercase letter, remaining chars are letters/digits/underscore).
func isExportedIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if r < 'A' || r > 'Z' {
				return false
			}
		} else {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
			if !ok {
				return false
			}
		}
	}
	return true
}

// pathToID converts a URL path to a PascalCase identifier fragment used when
// synthesising operationIds for operations that lack them.
func pathToID(p string) string {
	p = strings.ReplaceAll(p, "{", "")
	p = strings.ReplaceAll(p, "}", "")
	return naming.GoExported(strings.ReplaceAll(p, "/", "-"))
}

// diagWarn appends a DiagnosticWarning.
func (b *builder) diagWarn(msg, location string) {
	b.diags = append(b.diags, Diagnostic{
		Kind:     DiagnosticWarning,
		Message:  msg,
		Location: location,
	})
}
