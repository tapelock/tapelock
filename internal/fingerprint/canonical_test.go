package fingerprint

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCanonicalizeSortsObjectMembers(t *testing.T) {
	a, err := Canonicalize([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	b, err := Canonicalize([]byte(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}

	if !bytes.Equal(a, b) {
		t.Fatalf("member order affected output:\n a: %s\n b: %s", a, b)
	}
}

// TestCanonicalizeNormalizesNumbers guards the pitfall called out in the
// PRD (§ Piège JCS): 1 and 1.0 are the same number but different JSON
// text, and a naive canonicalizer would hash them differently.
func TestCanonicalizeNormalizesNumbers(t *testing.T) {
	a, err := Canonicalize([]byte(`{"n":1}`))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	b, err := Canonicalize([]byte(`{"n":1.0}`))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}

	if !bytes.Equal(a, b) {
		t.Fatalf("1 and 1.0 canonicalized differently:\n a: %s\n b: %s", a, b)
	}
}

func TestCanonicalizeUnescapesUnicode(t *testing.T) {
	escaped, err := Canonicalize([]byte(`{"s":"café"}`))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	literal, err := Canonicalize([]byte(`{"s":"café"}`))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}

	if !bytes.Equal(escaped, literal) {
		t.Fatalf("escaped and literal unicode canonicalized differently:\n escaped: %s\n literal: %s", escaped, literal)
	}
}

func TestCanonicalizeIsIdempotent(t *testing.T) {
	first, err := Canonicalize([]byte(`{"z":[3,2,1],"a":{"b":true,"c":null}}`))
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	second, err := Canonicalize(first)
	if err != nil {
		t.Fatalf("Canonicalize (second pass): %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("Canonicalize is not idempotent:\n first:  %s\n second: %s", first, second)
	}
}

func TestCanonicalizeRejectsMalformedJSON(t *testing.T) {
	if _, err := Canonicalize([]byte(`not json`)); err == nil {
		t.Fatal("Canonicalize: want error for malformed JSON, got nil")
	}
}

func FuzzCanonicalize(f *testing.F) {
	seeds := []string{
		`{}`,
		`[]`,
		`{"a":1,"b":2}`,
		`{"b":2,"a":1}`,
		`{"n":1.0}`,
		`{"s":"café"}`,
		`[1,2,3]`,
		`"just a string"`,
		`null`,
		`true`,
		`1e10`,
		`{"nested":{"array":[1,"two",3.0,null,true]}}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		out, err := Canonicalize([]byte(in))
		if err != nil {
			return // invalid JSON in, error out: acceptable outcome
		}
		if !json.Valid(out) {
			t.Fatalf("Canonicalize produced invalid JSON for input %q: %s", in, out)
		}

		again, err := Canonicalize(out)
		if err != nil {
			t.Fatalf("Canonicalize is not idempotent: re-canonicalizing its own output failed: %v", err)
		}
		if !bytes.Equal(out, again) {
			t.Fatalf("Canonicalize is not idempotent:\n first:  %s\n second: %s", out, again)
		}
	})
}
