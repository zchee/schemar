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

package naming_test

import (
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/zchee/schemar/naming"
)

func TestGoExported(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		// --- basic title-case ---
		"simple lowercase":           {input: "simple", want: "Simple"},
		"single uppercase preserved": {input: "Simple", want: "Simple"},
		"mixed case word":            {input: "myField", want: "MyField"},

		// --- underscore splitting ---
		"snake_case two words":          {input: "hello_world", want: "HelloWorld"},
		"snake_case multi word":         {input: "multi_word_field_name", want: "MultiWordFieldName"},
		"snake_case base_agent":         {input: "base_agent", want: "BaseAgent"},
		"snake_case system_instruction": {input: "system_instruction", want: "SystemInstruction"},
		"snake_case created_at":         {input: "created_at", want: "CreatedAt"},
		"snake_case updated_at":         {input: "updated_at", want: "UpdatedAt"},
		"snake_case has_more":           {input: "has_more", want: "HasMore"},
		"snake_case resource_type":      {input: "resource_type", want: "ResourceType"},
		"snake_case predefined_role":    {input: "predefined_role", want: "PredefinedRole"},

		// --- hyphen splitting ---
		"kebab-case two words": {input: "hello-world", want: "HelloWorld"},
		"user-agent":           {input: "user-agent", want: "UserAgent"},

		// --- dot splitting ---
		"dot separator": {input: "hello.world", want: "HelloWorld"},
		"content.type":  {input: "content.type", want: "ContentType"},

		// --- slash splitting ---
		"path separator": {input: "hello/world", want: "HelloWorld"},

		// --- space splitting ---
		"space separator": {input: "hello world", want: "HelloWorld"},

		// --- initialism: ID ---
		"bare id":    {input: "id", want: "ID"},
		"user_id":    {input: "user_id", want: "UserID"},
		"first_id":   {input: "first_id", want: "FirstID"},
		"last_id":    {input: "last_id", want: "LastID"},
		"request_id": {input: "request_id", want: "RequestID"},

		// --- initialism: URL ---
		"bare url":     {input: "url", want: "URL"},
		"base_url":     {input: "base_url", want: "BaseURL"},
		"callback_url": {input: "callback_url", want: "CallbackURL"},

		// --- initialism: API ---
		"bare api":    {input: "api", want: "API"},
		"api_key":     {input: "api_key", want: "APIKey"},
		"api_version": {input: "api_version", want: "APIVersion"},

		// --- initialism: HTTP ---
		"bare http":        {input: "http", want: "HTTP"},
		"http_client":      {input: "http_client", want: "HTTPClient"},
		"http_status_code": {input: "http_status_code", want: "HTTPStatusCode"},

		// --- initialism: JSON ---
		"bare json":   {input: "json", want: "JSON"},
		"json_body":   {input: "json_body", want: "JSONBody"},
		"json_schema": {input: "json_schema", want: "JSONSchema"},

		// --- initialism: IO ---
		"bare io":   {input: "io", want: "IO"},
		"io_reader": {input: "io_reader", want: "IOReader"},

		// --- initialism: URI ---
		"bare uri": {input: "uri", want: "URI"},
		"uri_path": {input: "uri_path", want: "URIPath"},

		// --- initialism: other common ones ---
		"bare uuid": {input: "uuid", want: "UUID"},
		"bare sql":  {input: "sql", want: "SQL"},
		"bare tls":  {input: "tls", want: "TLS"},
		"bare xml":  {input: "xml", want: "XML"},
		"bare html": {input: "html", want: "HTML"},
		"bare dns":  {input: "dns", want: "DNS"},
		"bare grpc": {input: "grpc", want: "GRPC"},
		"bare db":   {input: "db", want: "DB"},

		// --- multi-initialism ---
		"api_url":         {input: "api_url", want: "APIURL"},
		"json_schema_url": {input: "json_schema_url", want: "JSONSchemaURL"},
		"http_api_url":    {input: "http_api_url", want: "HTTPAPIURL"},

		// --- already-PascalCase schema names (no separator) ---
		"PascalCase no split":                {input: "AddUploadPartRequest", want: "AddUploadPartRequest"},
		"already exported Agent":             {input: "Agent", want: "Agent"},
		"already exported URL":               {input: "URL", want: "URL"},
		"already exported CreateInteraction": {input: "CreateInteraction", want: "CreateInteraction"},

		// --- idempotence: applying twice gives same result ---
		"idempotent UserID":     {input: "UserID", want: "UserID"},
		"idempotent BaseAgent":  {input: "BaseAgent", want: "BaseAgent"},
		"idempotent HTTPClient": {input: "HTTPClient", want: "HTTPClient"},

		// --- edge cases ---
		"empty string":         {input: "", want: "_"},
		"only underscore":      {input: "_", want: "_"},
		"multiple underscores": {input: "___", want: "_"},
		"leading digit":        {input: "123abc", want: "X123abc"},
		"pure digit":           {input: "42", want: "X42"},
		"mixed separators":     {input: "hello-world.foo/bar", want: "HelloWorldFooBar"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := naming.GoExported(tc.input)
			if diff := gocmp.Diff(tc.want, got); diff != "" {
				t.Errorf("GoExported(%q) mismatch (-want +got):\n%s", tc.input, diff)
			}
		})
	}
}

func TestGoField(t *testing.T) {
	t.Parallel()

	// GoField and GoExported share the same conversion logic; this test
	// confirms the contract for struct field name use-cases specifically.
	tests := map[string]struct {
		input string
		want  string
	}{
		"snake_case field base_agent":         {input: "base_agent", want: "BaseAgent"},
		"snake_case field system_instruction": {input: "system_instruction", want: "SystemInstruction"},
		"id field":                            {input: "id", want: "ID"},
		"user_id field":                       {input: "user_id", want: "UserID"},
		"created_at field":                    {input: "created_at", want: "CreatedAt"},
		"has_more field":                      {input: "has_more", want: "HasMore"},
		"api_key field":                       {input: "api_key", want: "APIKey"},
		"base_url field":                      {input: "base_url", want: "BaseURL"},
		"json_schema field":                   {input: "json_schema", want: "JSONSchema"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := naming.GoField(tc.input)
			if diff := gocmp.Diff(tc.want, got); diff != "" {
				t.Errorf("GoField(%q) mismatch (-want +got):\n%s", tc.input, diff)
			}
		})
	}
}

func TestGoPackageName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"openai api title": {input: "OpenAI API", want: "openaiapi"},
		"gemini api title": {input: "Gemini API", want: "geminiapi"},
		"snake_case title": {input: "my_service", want: "myservice"},
		"hyphen title":     {input: "my-service", want: "myservice"},
		"empty":            {input: "", want: "apiclient"},
		"only separators":  {input: "___", want: "apiclient"},
		"leading digit":    {input: "2024service", want: "x2024service"},
		"mixed separators": {input: "My API Service", want: "myapiservice"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := naming.GoPackageName(tc.input)
			if diff := gocmp.Diff(tc.want, got); diff != "" {
				t.Errorf("GoPackageName(%q) mismatch (-want +got):\n%s", tc.input, diff)
			}
		})
	}
}
