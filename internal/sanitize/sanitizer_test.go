package sanitize

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSanitizeReplacesUUIDsConsistently(t *testing.T) {
	s := New(UUID)

	a, err := s.Sanitize([]byte(`{"request_id":"550e8400-e29b-41d4-a716-446655440000","model":"gpt-4o-mini"}`))
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	b, err := s.Sanitize([]byte(`{"request_id":"11111111-2222-3333-4444-555555555555","model":"gpt-4o-mini"}`))
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}

	if !bytes.Equal(a, b) {
		t.Fatalf("two different UUIDs did not sanitize to the same output:\n a: %s\n b: %s", a, b)
	}
}

func TestSanitizeReplacesTimestamps(t *testing.T) {
	s := New(Timestamp)

	out, err := s.Sanitize([]byte(`{"created_at":"2026-09-19T10:00:00.123Z"}`))
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}

	want := `{"created_at":"<TIMESTAMP>"}`
	if string(out) != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

func TestSanitizeCustomRule(t *testing.T) {
	rule, err := NewRule(`ORD-\d+`, "<ORDER>")
	if err != nil {
		t.Fatalf("NewRule: %v", err)
	}
	s := New(rule)

	out, err := s.Sanitize([]byte(`{"note":"see order ORD-4821 for details"}`))
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}

	want := `{"note":"see order <ORDER> for details"}`
	if string(out) != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

func TestSanitizeLeavesObjectKeysAlone(t *testing.T) {
	s := New(UUID)

	// The map key itself looks like a UUID; only string *values* should be
	// rewritten, never keys.
	in := `{"550e8400-e29b-41d4-a716-446655440000":"550e8400-e29b-41d4-a716-446655440000"}`
	out, err := s.Sanitize([]byte(in))
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}

	want := `{"550e8400-e29b-41d4-a716-446655440000":"<UUID>"}`
	if string(out) != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

func TestSanitizeNumbersUnaffected(t *testing.T) {
	s := New(UUID, Timestamp)

	in := `{"max_tokens":1000,"temperature":0.7}`
	out, err := s.Sanitize([]byte(in))
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()

	var got map[string]any
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got["max_tokens"].(json.Number).String() != "1000" {
		t.Fatalf("max_tokens changed: got %v", got["max_tokens"])
	}
	if got["temperature"].(json.Number).String() != "0.7" {
		t.Fatalf("temperature changed: got %v", got["temperature"])
	}
}

func TestSanitizeDoesNotMutateInput(t *testing.T) {
	s := New(UUID)

	in := []byte(`{"id":"550e8400-e29b-41d4-a716-446655440000"}`)
	original := append([]byte(nil), in...)

	if _, err := s.Sanitize(in); err != nil {
		t.Fatalf("Sanitize: %v", err)
	}

	if !bytes.Equal(in, original) {
		t.Fatalf("Sanitize mutated its input: got %s, want %s", in, original)
	}
}

func TestSanitizeRejectsMalformedJSON(t *testing.T) {
	s := New(UUID)
	if _, err := s.Sanitize([]byte(`not json`)); err == nil {
		t.Fatal("Sanitize: want error for malformed JSON, got nil")
	}
}

func FuzzSanitize(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"id":"550e8400-e29b-41d4-a716-446655440000"}`,
		`{"created_at":"2026-09-19T10:00:00Z","nested":{"id":"550e8400-e29b-41d4-a716-446655440000"}}`,
		`[1,2,3]`,
		`"plain string"`,
		`null`,
		`true`,
		`1.5`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	s := New(UUID, Timestamp)

	f.Fuzz(func(t *testing.T, in string) {
		out, err := s.Sanitize([]byte(in))
		if err != nil {
			return // invalid JSON in, error out: acceptable outcome
		}
		if !json.Valid(out) {
			t.Fatalf("Sanitize produced invalid JSON for input %q: %s", in, out)
		}

		again, err := s.Sanitize(out)
		if err != nil {
			t.Fatalf("Sanitize is not idempotent: re-sanitizing its own output failed: %v", err)
		}
		if !bytes.Equal(out, again) {
			t.Fatalf("Sanitize is not idempotent:\n first:  %s\n second: %s", out, again)
		}
	})
}
