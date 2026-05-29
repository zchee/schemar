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

package oneof_test

import (
	"errors"
	"testing"

	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	gocmp "github.com/google/go-cmp/cmp"
	gocmpopts "github.com/google/go-cmp/cmp/cmpopts"

	"github.com/zchee/schemar/oneof"
)

// ── Concrete test types (mirrors emitted Strategy B shape) ────────────────

// modelParams is a concrete variant type for tests (represents a "model"
// interaction request). Only known fields; strict decode rejects unknowns.
type modelParams struct {
	Type    string `json:"type"`
	ModelID string `json:"model_id"`
}

// agentParams is a concrete variant type for tests (represents an "agent"
// interaction request). Only known fields.
type agentParams struct {
	Type    string `json:"type"`
	AgentID string `json:"agent_id"`
}

// discriminatedUnion is a Strategy B wrapper with a discriminator field.
// Its MarshalJSON/UnmarshalJSON mirror what the emitter generates for a
// oneOf with discriminator.propertyName = "type".
type discriminatedUnion struct {
	Model *modelParams
	Agent *agentParams
	Raw   jsontext.Value
}

func (u *discriminatedUnion) MarshalJSON() ([]byte, error) {
	switch {
	case u.Model != nil:
		return json.Marshal(u.Model)
	case u.Agent != nil:
		return json.Marshal(u.Agent)
	default:
		if len(u.Raw) > 0 {
			return []byte(u.Raw), nil
		}
		return []byte("null"), nil
	}
}

func (u *discriminatedUnion) UnmarshalJSON(b []byte) error {
	idx, err := oneof.MatchDiscriminator(b, "type", []string{"model", "agent"})
	if err != nil {
		if errors.Is(err, oneof.ErrNoVariantMatched) {
			// Known discriminator field but unknown value — retain raw.
			u.Raw = b
			return nil
		}
		return err
	}
	switch idx {
	case 0: // "model"
		var v modelParams
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		u.Model = &v
	case 1: // "agent"
		var v agentParams
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		u.Agent = &v
	default:
		// Discriminator field absent — retain raw for caller inspection.
		u.Raw = b
	}
	return nil
}

// trialDecodeUnion is a Strategy B wrapper without a discriminator.
// UnmarshalJSON uses strict trial-decode via oneof.UnmarshalTrial +
// oneof.StrictUnmarshal, exactly as the emitter would generate.
type trialDecodeUnion struct {
	Model *modelParams
	Agent *agentParams
	Raw   jsontext.Value
}

func (u *trialDecodeUnion) MarshalJSON() ([]byte, error) {
	switch {
	case u.Model != nil:
		return json.Marshal(u.Model)
	case u.Agent != nil:
		return json.Marshal(u.Agent)
	default:
		if len(u.Raw) > 0 {
			return []byte(u.Raw), nil
		}
		return []byte("null"), nil
	}
}

func (u *trialDecodeUnion) UnmarshalJSON(b []byte) error {
	// Allocate targets before the trial so we can assign the winner.
	var m modelParams
	var a agentParams

	idx, err := oneof.UnmarshalTrial(b, []func([]byte) error{
		func(data []byte) error { return oneof.StrictUnmarshal(data, &m) },
		func(data []byte) error { return oneof.StrictUnmarshal(data, &a) },
	})
	if err != nil {
		return err
	}
	switch idx {
	case 0:
		u.Model = &m
	case 1:
		u.Agent = &a
	default:
		// No variant matched; retain raw bytes.
		u.Raw = b
	}
	return nil
}

// ── Classify tests ────────────────────────────────────────────────────────

// TestClassify verifies that Classify assigns StrategyB when all variants are
// object references and StrategyA when any variant is a primitive JSON type,
// and that IsOneOf and Discriminator are passed through unchanged.
func TestClassify(t *testing.T) {
	t.Parallel()

	refVariants := []oneof.UnionVariant{
		{TypeName: "ModelParams", IsRef: true},
		{TypeName: "AgentParams", IsRef: true},
	}
	primitiveVariants := []oneof.UnionVariant{
		{TypeName: "ModelParams", IsRef: true},
		{IsPrimitive: true, JSONType: "string"},
	}
	allPrimitives := []oneof.UnionVariant{
		{IsPrimitive: true, JSONType: "string"},
		{IsPrimitive: true, JSONType: "integer"},
	}

	tests := map[string]struct {
		isOneOf       bool
		discriminator string
		variants      []oneof.UnionVariant
		wantStrategy  oneof.Strategy
	}{
		"oneOf all refs → StrategyB": {
			isOneOf:      true,
			variants:     refVariants,
			wantStrategy: oneof.StrategyB,
		},
		"anyOf all refs → StrategyB": {
			isOneOf:      false,
			variants:     refVariants,
			wantStrategy: oneof.StrategyB,
		},
		"oneOf with discriminator → StrategyB": {
			isOneOf:       true,
			discriminator: "type",
			variants:      refVariants,
			wantStrategy:  oneof.StrategyB,
		},
		"oneOf one primitive → StrategyA": {
			isOneOf:      true,
			variants:     primitiveVariants,
			wantStrategy: oneof.StrategyA,
		},
		"oneOf all primitives → StrategyA": {
			isOneOf:      true,
			variants:     allPrimitives,
			wantStrategy: oneof.StrategyA,
		},
		"anyOf one primitive → StrategyA": {
			isOneOf:      false,
			variants:     primitiveVariants,
			wantStrategy: oneof.StrategyA,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			info := oneof.Classify(tc.isOneOf, tc.discriminator, tc.variants)
			if diff := gocmp.Diff(tc.wantStrategy, info.Strategy); diff != "" {
				t.Errorf("Classify() strategy mismatch (-want +got):\n%s", diff)
			}
			if diff := gocmp.Diff(tc.isOneOf, info.IsOneOf); diff != "" {
				t.Errorf("Classify() IsOneOf mismatch (-want +got):\n%s", diff)
			}
			if diff := gocmp.Diff(tc.discriminator, info.Discriminator); diff != "" {
				t.Errorf("Classify() Discriminator mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestStrategyADiagnostic verifies the FIXME constant matches the
// canonical form checked by the emitter template.
func TestStrategyADiagnostic(t *testing.T) {
	t.Parallel()
	const want = "// FIXME: oneOf with primitives — type-safe Strategy B unavailable; using any."
	if diff := gocmp.Diff(want, oneof.StrategyADiagnostic); diff != "" {
		t.Errorf("StrategyADiagnostic mismatch (-want +got):\n%s", diff)
	}

	// A union with a primitive variant must produce StrategyA whose emitter
	// would emit the FIXME diagnostic.
	variants := []oneof.UnionVariant{
		{TypeName: "ModelParams", IsRef: true},
		{IsPrimitive: true, JSONType: "string"},
	}
	info := oneof.Classify(true, "", variants)
	if info.Strategy != oneof.StrategyA {
		t.Errorf("expected StrategyA for primitive union, got %v", info.Strategy)
	}
}

// ── MatchDiscriminator tests ──────────────────────────────────────────────

// TestMatchDiscriminator verifies that MatchDiscriminator returns the correct
// variant index when the discriminator field is present and its value is in the
// candidate list, returns -1 with no error when the field is absent, and returns
// [oneof.ErrNoVariantMatched] when the field is present but the value is unknown.
func TestMatchDiscriminator(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		data       []byte
		field      string
		values     []string
		wantIdx    int
		wantErr    error // specific sentinel; checked with errors.Is
		wantErrAny bool  // true when any non-nil error is acceptable
	}{
		"field present, first value matches": {
			data:    []byte(`{"type":"model","model_id":"m1"}`),
			field:   "type",
			values:  []string{"model", "agent"},
			wantIdx: 0,
		},
		"field present, second value matches": {
			data:    []byte(`{"type":"agent","agent_id":"a1"}`),
			field:   "type",
			values:  []string{"model", "agent"},
			wantIdx: 1,
		},
		"field absent → -1 no error": {
			data:    []byte(`{"model_id":"m1"}`),
			field:   "type",
			values:  []string{"model", "agent"},
			wantIdx: -1,
		},
		"field present, value not in list → ErrNoVariantMatched": {
			data:    []byte(`{"type":"unknown","x":1}`),
			field:   "type",
			values:  []string{"model", "agent"},
			wantIdx: -1,
			wantErr: oneof.ErrNoVariantMatched,
		},
		"empty values list → ErrNoVariantMatched": {
			data:    []byte(`{"type":"model"}`),
			field:   "type",
			values:  []string{},
			wantIdx: -1,
			wantErr: oneof.ErrNoVariantMatched,
		},
		"nested object, top-level field matched": {
			data:    []byte(`{"type":"model","nested":{"a":1}}`),
			field:   "type",
			values:  []string{"model"},
			wantIdx: 0,
		},
		"invalid JSON → any error": {
			data:       []byte(`not-json`),
			field:      "type",
			values:     []string{"model"},
			wantIdx:    -1,
			wantErrAny: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := oneof.MatchDiscriminator(tc.data, tc.field, tc.values)
			if tc.wantErrAny {
				if err == nil {
					t.Fatalf("MatchDiscriminator() expected non-nil error, got nil")
				}
				return
			}
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("MatchDiscriminator() expected error %v, got nil", tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("MatchDiscriminator() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("MatchDiscriminator() unexpected error: %v", err)
			}
			if diff := gocmp.Diff(tc.wantIdx, got); diff != "" {
				t.Errorf("MatchDiscriminator() index mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// ── Strategy B discriminated union tests ─────────────────────────────────

// TestDiscriminatedUnion_Unmarshal verifies that a Strategy B discriminated
// union routes JSON to the correct typed field when the discriminator matches a
// known variant, and retains the raw bytes when the discriminator field is
// absent or holds an unrecognized value.
func TestDiscriminatedUnion_Unmarshal(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input     []byte
		wantModel *modelParams
		wantAgent *agentParams
		wantRaw   bool // true when Raw should be retained
	}{
		"type=model → Model field populated": {
			input:     []byte(`{"type":"model","model_id":"m-123"}`),
			wantModel: &modelParams{Type: "model", ModelID: "m-123"},
		},
		"type=agent → Agent field populated": {
			input:     []byte(`{"type":"agent","agent_id":"a-456"}`),
			wantAgent: &agentParams{Type: "agent", AgentID: "a-456"},
		},
		"discriminator field absent → raw retained": {
			input:   []byte(`{"model_id":"m-no-type"}`),
			wantRaw: true,
		},
		"discriminator present but unknown value → raw retained": {
			input:   []byte(`{"type":"future_variant","x":1}`),
			wantRaw: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var u discriminatedUnion
			if err := json.Unmarshal(tc.input, &u); err != nil {
				t.Fatalf("Unmarshal() unexpected error: %v", err)
			}
			if diff := gocmp.Diff(tc.wantModel, u.Model, gocmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Model mismatch (-want +got):\n%s", diff)
			}
			if diff := gocmp.Diff(tc.wantAgent, u.Agent, gocmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Agent mismatch (-want +got):\n%s", diff)
			}
			if tc.wantRaw && len(u.Raw) == 0 {
				t.Error("expected Raw to be retained but it is empty")
			}
			if !tc.wantRaw && len(u.Raw) > 0 {
				t.Errorf("expected Raw to be empty but got: %s", u.Raw)
			}
		})
	}
}

// TestDiscriminatedUnion_Marshal verifies that marshaling a discriminated union
// emits the active variant's own field set, and that a union holding only raw
// bytes passes those bytes through unchanged.
func TestDiscriminatedUnion_Marshal(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		union   discriminatedUnion
		wantKey string // JSON key that must appear in output
	}{
		"model variant marshals": {
			union:   discriminatedUnion{Model: &modelParams{Type: "model", ModelID: "m-1"}},
			wantKey: "model_id",
		},
		"agent variant marshals": {
			union:   discriminatedUnion{Agent: &agentParams{Type: "agent", AgentID: "a-1"}},
			wantKey: "agent_id",
		},
		"raw passthrough when both nil": {
			union:   discriminatedUnion{Raw: jsontext.Value(`{"type":"future","z":true}`)},
			wantKey: "type", // "future" is the VALUE of type; "type" is the key
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := json.Marshal(&tc.union)
			if err != nil {
				t.Fatalf("Marshal() unexpected error: %v", err)
			}
			// Verify the expected key appears in the marshaled output.
			var raw map[string]jsontext.Value
			if err := json.Unmarshal(out, &raw); err != nil {
				t.Fatalf("re-unmarshal of output failed: %v", err)
			}
			if _, ok := raw[tc.wantKey]; !ok {
				t.Errorf("marshaled output %s does not contain expected key %q", out, tc.wantKey)
			}
		})
	}
}

// ── Strategy B trial-decode union tests ──────────────────────────────────

// TestTrialDecodeUnion_Unmarshal verifies that a Strategy B trial-decode union
// selects the first variant whose strict decoder accepts the input, and retains
// the raw bytes when every strict decoder rejects the object.
func TestTrialDecodeUnion_Unmarshal(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input     []byte
		wantModel *modelParams
		wantAgent *agentParams
		wantRaw   bool
	}{
		"model fields present → Model populated": {
			input:     []byte(`{"type":"model","model_id":"m-trial"}`),
			wantModel: &modelParams{Type: "model", ModelID: "m-trial"},
		},
		"agent fields present → Agent populated": {
			input:     []byte(`{"type":"agent","agent_id":"a-trial"}`),
			wantAgent: &agentParams{Type: "agent", AgentID: "a-trial"},
		},
		"unknown fields → raw retained (strict mode rejects both)": {
			// Has neither model_id nor agent_id; strict unmarshal rejects both.
			// Actually, strict mode will reject *any* unknown field, so an
			// object with only unknown fields will fail all decoders → raw.
			input:   []byte(`{"completely":"unknown","x":99}`),
			wantRaw: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var u trialDecodeUnion
			if err := json.Unmarshal(tc.input, &u); err != nil {
				t.Fatalf("Unmarshal() unexpected error: %v", err)
			}
			if diff := gocmp.Diff(tc.wantModel, u.Model, gocmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Model mismatch (-want +got):\n%s", diff)
			}
			if diff := gocmp.Diff(tc.wantAgent, u.Agent, gocmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Agent mismatch (-want +got):\n%s", diff)
			}
			if tc.wantRaw && len(u.Raw) == 0 {
				t.Error("expected Raw to be retained but it is empty")
			}
			if !tc.wantRaw && len(u.Raw) > 0 {
				t.Errorf("expected Raw to be empty but got: %s", u.Raw)
			}
		})
	}
}

// TestRawRetained_MarshalRoundtrip verifies that a union where no variant
// matched retains and re-emits the raw bytes faithfully.
func TestRawRetained_MarshalRoundtrip(t *testing.T) {
	t.Parallel()

	original := []byte(`{"type":"future","new_field":"value","count":42}`)

	var u discriminatedUnion
	if err := json.Unmarshal(original, &u); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(u.Raw) == 0 {
		t.Fatal("expected Raw to be populated for unknown discriminator value")
	}

	// Re-marshal should reproduce the raw bytes.
	out, err := json.Marshal(&u)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Canonicalize both before comparing to ignore whitespace differences.
	wantVal := jsontext.Value(original)
	gotVal := jsontext.Value(out)
	if err := wantVal.Canonicalize(); err != nil {
		t.Fatalf("canonicalize want: %v", err)
	}
	if err := gotVal.Canonicalize(); err != nil {
		t.Fatalf("canonicalize got: %v", err)
	}
	if diff := gocmp.Diff(string(wantVal), string(gotVal)); diff != "" {
		t.Errorf("raw round-trip mismatch (-want +got):\n%s", diff)
	}
}

// TestUnmarshalTrial_AllFail verifies -1 is returned when every decoder
// rejects the data.
func TestUnmarshalTrial_AllFail(t *testing.T) {
	t.Parallel()

	data := []byte(`{"completely":"unknown","x":99}`)

	var m modelParams
	var a agentParams

	idx, err := oneof.UnmarshalTrial(data, []func([]byte) error{
		func(b []byte) error { return oneof.StrictUnmarshal(b, &m) },
		func(b []byte) error { return oneof.StrictUnmarshal(b, &a) },
	})
	if err != nil {
		t.Fatalf("UnmarshalTrial: unexpected error: %v", err)
	}
	if idx != -1 {
		t.Errorf("expected idx=-1, got %d", idx)
	}
}

// TestUnmarshalTrial_FirstWins verifies that the first matching decoder wins.
func TestUnmarshalTrial_FirstWins(t *testing.T) {
	t.Parallel()

	data := []byte(`{"type":"model","model_id":"m-1"}`)

	var m modelParams
	var a agentParams

	idx, err := oneof.UnmarshalTrial(data, []func([]byte) error{
		func(b []byte) error { return oneof.StrictUnmarshal(b, &m) },
		func(b []byte) error { return oneof.StrictUnmarshal(b, &a) },
	})
	if err != nil {
		t.Fatalf("UnmarshalTrial: unexpected error: %v", err)
	}
	if idx != 0 {
		t.Errorf("expected idx=0 (first decoder wins), got %d", idx)
	}
	if m.ModelID != "m-1" {
		t.Errorf("expected ModelID=m-1, got %q", m.ModelID)
	}
}
