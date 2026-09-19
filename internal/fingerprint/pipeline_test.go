package fingerprint_test

// These tests exercise sanitize -> fingerprint.Canonicalize -> fingerprint.Compute
// end to end, matching the scenarios called out in mvp.md §14: identical
// semantic requests must hash the same, different requests must not, and
// sanitization must be what makes volatile values (UUIDs, timestamps)
// disappear from the hash.

import (
	"testing"

	"github.com/tapelock/tapelock/internal/fingerprint"
	"github.com/tapelock/tapelock/internal/sanitize"
)

func fingerprintBody(t *testing.T, body string) fingerprint.Fingerprint {
	t.Helper()

	sanitizer := sanitize.New(sanitize.UUID, sanitize.Timestamp)
	sanitized, err := sanitizer.Sanitize([]byte(body))
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}

	canonical, err := fingerprint.Canonicalize(sanitized)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}

	return fingerprint.Compute(fingerprint.Input{
		Method:        "POST",
		Path:          "/v1/chat/completions",
		CanonicalBody: canonical,
	})
}

func TestPipelineSameSemanticRequestSameHash(t *testing.T) {
	// Differ only in key order and in a request_id UUID: same fingerprint.
	a := fingerprintBody(t, `{"model":"gpt-4o-mini","request_id":"11111111-1111-1111-1111-111111111111","messages":[{"role":"user","content":"hi"}]}`)
	b := fingerprintBody(t, `{"request_id":"22222222-2222-2222-2222-222222222222","model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)

	if a.Hash != b.Hash {
		t.Fatalf("semantically identical requests hashed differently: %q vs %q", a.Hash, b.Hash)
	}
}

func TestPipelineDifferentContentDifferentHash(t *testing.T) {
	a := fingerprintBody(t, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	b := fingerprintBody(t, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"bye"}]}`)

	if a.Hash == b.Hash {
		t.Fatalf("different message content hashed the same: %q", a.Hash)
	}
}

func TestPipelineTimestampsSanitizedToSameHash(t *testing.T) {
	a := fingerprintBody(t, `{"model":"gpt-4o-mini","created_at":"2026-09-19T10:00:00Z","messages":[]}`)
	b := fingerprintBody(t, `{"model":"gpt-4o-mini","created_at":"2026-09-20T11:30:05Z","messages":[]}`)

	if a.Hash != b.Hash {
		t.Fatalf("requests differing only by timestamp hashed differently: %q vs %q", a.Hash, b.Hash)
	}
}

// TestPipelineWithoutSanitizationDiffersOnUUID is the control for the test
// above: without sanitization, two otherwise-identical requests carrying
// different UUIDs must NOT collide. This is what makes over-normalization
// (see PRD "sur-normalisation") a real risk worth testing for, not a
// theoretical one.
func TestPipelineWithoutSanitizationDiffersOnUUID(t *testing.T) {
	fp := func(body string) fingerprint.Fingerprint {
		t.Helper()
		canonical, err := fingerprint.Canonicalize([]byte(body))
		if err != nil {
			t.Fatalf("Canonicalize: %v", err)
		}
		return fingerprint.Compute(fingerprint.Input{Method: "POST", Path: "/v1/chat/completions", CanonicalBody: canonical})
	}

	a := fp(`{"request_id":"11111111-1111-1111-1111-111111111111"}`)
	b := fp(`{"request_id":"22222222-2222-2222-2222-222222222222"}`)

	if a.Hash == b.Hash {
		t.Fatalf("expected different hashes without sanitization, got same hash %q", a.Hash)
	}
}
