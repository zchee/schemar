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

// Package naming converts arbitrary OpenAPI identifier strings to valid,
// idiomatic exported Go identifiers following the Go style guide.
package naming

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// initialisations maps lowercase words to their canonical all-caps Go form.
// The list follows the Google Go Style Guide's initialism table.
var initialisations = map[string]string{
	"acl":   "ACL",
	"api":   "API",
	"ascii": "ASCII",
	"cpu":   "CPU",
	"css":   "CSS",
	"db":    "DB",
	"dns":   "DNS",
	"eof":   "EOF",
	"grpc":  "GRPC",
	"guid":  "GUID",
	"html":  "HTML",
	"http":  "HTTP",
	"https": "HTTPS",
	"id":    "ID",
	"io":    "IO",
	"ip":    "IP",
	"json":  "JSON",
	"lhs":   "LHS",
	"qps":   "QPS",
	"ram":   "RAM",
	"rhs":   "RHS",
	"rpc":   "RPC",
	"sla":   "SLA",
	"smtp":  "SMTP",
	"sql":   "SQL",
	"ssh":   "SSH",
	"tcp":   "TCP",
	"tls":   "TLS",
	"ttl":   "TTL",
	"udp":   "UDP",
	"ui":    "UI",
	"uid":   "UID",
	"uuid":  "UUID",
	"uri":   "URI",
	"url":   "URL",
	"utf8":  "UTF8",
	"vm":    "VM",
	"xml":   "XML",
	"xss":   "XSS",
	"xsrf":  "XSRF",
}

// goKeywords is the complete set of Go reserved keywords. After PascalCase
// conversion results always start with an uppercase letter, so this check
// is a defensive safety net for unusual inputs.
var goKeywords = map[string]bool{
	"break":       true,
	"case":        true,
	"chan":        true,
	"const":       true,
	"continue":    true,
	"default":     true,
	"defer":       true,
	"else":        true,
	"fallthrough": true,
	"for":         true,
	"func":        true,
	"go":          true,
	"goto":        true,
	"if":          true,
	"import":      true,
	"interface":   true,
	"map":         true,
	"package":     true,
	"range":       true,
	"return":      true,
	"select":      true,
	"struct":      true,
	"switch":      true,
	"type":        true,
	"var":         true,
}

// GoExported converts s to an exported Go identifier (PascalCase) following
// the Go style guide, with initialism preservation and keyword-collision
// avoidance. It is intended for use on schema and type names.
func GoExported(s string) string {
	return convert(s)
}

// GoField converts s to an exported Go struct field name following the Go
// style guide. The conversion is identical to GoExported and is provided as
// a distinct function to clarify call-site intent.
func GoField(s string) string {
	return convert(s)
}

// GoPackageName converts s to a valid Go package name: all lowercase,
// only letters and digits, no underscores. Suitable for use in package
// declarations and import paths derived from an OpenAPI info.title.
func GoPackageName(s string) string {
	if s == "" {
		return "apiclient"
	}
	parts := splitWords(s)
	if len(parts) == 0 {
		return "apiclient"
	}
	var b strings.Builder
	for _, p := range parts {
		for _, r := range p {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(unicode.ToLower(r))
			}
		}
	}
	result := b.String()
	if result == "" {
		return "apiclient"
	}
	// Package names must not start with a digit.
	if r, _ := utf8.DecodeRuneInString(result); unicode.IsDigit(r) {
		result = "x" + result
	}
	return result
}

// convert performs the shared PascalCase conversion with initialism
// preservation and keyword-collision avoidance.
func convert(s string) string {
	if s == "" {
		return "_"
	}

	parts := splitWords(s)
	if len(parts) == 0 {
		return "_"
	}

	var b strings.Builder
	for _, part := range parts {
		lower := strings.ToLower(part)
		if init, ok := initialisations[lower]; ok {
			b.WriteString(init)
		} else {
			b.WriteString(titleFirst(part))
		}
	}

	result := b.String()
	if result == "" {
		return "_"
	}

	// Prepend "X" when the first rune is a digit to form a valid identifier.
	if r, _ := utf8.DecodeRuneInString(result); unicode.IsDigit(r) {
		result = "X" + result
	}

	// Append underscore when result exactly equals a Go keyword (safety net).
	if goKeywords[result] {
		result += "_"
	}

	return result
}

// splitWords splits s into word segments for PascalCase conversion.
// Any rune that is not a Unicode letter or digit is treated as a word
// separator, including underscore, hyphen, dot, slash, space, colon,
// square brackets, asterisk, and any other punctuation or symbol.
// This handles the full range of OpenAPI identifier characters: schema
// names like "include[]", enum values like "step_details.tool_calls[*].content",
// and aspect-ratio strings like "1:1".
// Empty segments produced by consecutive separators are dropped.
func splitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// titleFirst returns s with its first Unicode letter uppercased. The
// remaining characters are left unchanged so that already-PascalCase
// segments (e.g. "AddUploadPartRequest") are preserved intact.
func titleFirst(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
