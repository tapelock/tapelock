package fingerprint

import (
	"regexp"
	"testing"
)

var hashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func TestComputeHashFormat(t *testing.T) {
	fp := Compute(Input{Method: "POST", Path: "/v1/chat/completions", CanonicalBody: []byte(`{}`)})

	if !hashPattern.MatchString(fp.Hash) {
		t.Fatalf("Hash %q does not match sha256:<64 hex chars>", fp.Hash)
	}
}

func TestComputeIsDeterministic(t *testing.T) {
	in := Input{
		Method:        "POST",
		Path:          "/v1/chat/completions",
		Headers:       map[string][]string{"content-type": {"application/json"}},
		CanonicalBody: []byte(`{"model":"gpt-4o-mini"}`),
	}

	a := Compute(in)
	b := Compute(in)

	if a.Hash != b.Hash {
		t.Fatalf("Compute is not deterministic: %q vs %q", a.Hash, b.Hash)
	}
}

func TestComputeDiffersOn(t *testing.T) {
	base := Input{
		Method:        "POST",
		Path:          "/v1/chat/completions",
		Headers:       map[string][]string{"content-type": {"application/json"}},
		CanonicalBody: []byte(`{"model":"gpt-4o-mini"}`),
	}

	variants := map[string]Input{
		"method": func() Input { i := base; i.Method = "GET"; return i }(),
		"path":   func() Input { i := base; i.Path = "/v1/other"; return i }(),
		"body":   func() Input { i := base; i.CanonicalBody = []byte(`{"model":"other"}`); return i }(),
		"headers": func() Input {
			i := base
			i.Headers = map[string][]string{"content-type": {"text/plain"}}
			return i
		}(),
	}

	baseHash := Compute(base).Hash
	for name, variant := range variants {
		t.Run(name, func(t *testing.T) {
			got := Compute(variant).Hash
			if got == baseHash {
				t.Fatalf("changing %s did not change the hash (got %q)", name, got)
			}
		})
	}
}

func TestComputeHeaderMapIterationOrderDoesNotMatter(t *testing.T) {
	headers := map[string][]string{
		"content-type":   {"application/json"},
		"accept":         {"application/json"},
		"anthropic-beta": {"tools-2024-04-04"},
	}

	in := Input{Method: "POST", Path: "/v1/messages", Headers: headers, CanonicalBody: []byte(`{}`)}

	// Map iteration order is randomized by the Go runtime; calling Compute
	// repeatedly on the same map exercises that randomness. canonicalHeaders
	// sorts by name, so the hash must stay stable across calls.
	want := Compute(in).Hash
	for i := 0; i < 20; i++ {
		if got := Compute(in).Hash; got != want {
			t.Fatalf("hash changed across calls: %q vs %q", got, want)
		}
	}
}
