// Package fingerprint turns a sanitized, canonicalized request into a stable
// SHA-256 key: sanitize -> JCS canonicalization -> hash. This is Tapelock's
// core differentiator, so it stays pure (no I/O) and is exercised with
// table-driven and fuzz tests rather than integration tests.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Fingerprint identifies a semantically equivalent request. Two requests
// that differ only in fields sanitize.Sanitizer replaces (UUIDs,
// timestamps, custom regexes) or in JSON member order produce the same
// Fingerprint.
type Fingerprint struct {
	// Hash is "sha256:<hex>", computed over the canonical request.
	Hash string

	// Occurrence is the 0-based count of prior requests in the same
	// recording session that produced this same Hash. It lets agent loops
	// that repeat an identical request replay a different response each
	// time (see cassette.Interaction.RequestHash, which stores Hash alone:
	// occurrence is tracked by the caller, e.g. the cassette manager).
	Occurrence int
}

// Input is what Compute hashes into a Fingerprint. The body must already be
// sanitized and JCS-canonicalized (see Canonicalize). Compute itself does
// no transformation, it only hashes.
type Input struct {
	Method string
	Path   string

	// Headers must already be filtered down to the configured allowlist.
	// Compute deliberately has no opinion on which headers matter: hashing
	// everything (Authorization, Date, User-Agent, Content-Length, ...)
	// would make replay fragile across harmless client differences (see
	// mvp.md §8).
	Headers map[string][]string

	// CanonicalBody is the request body after sanitize.Sanitizer and
	// Canonicalize have both run.
	CanonicalBody []byte
}

// Compute hashes Input into a Fingerprint. It is a pure function: the same
// Input always produces the same Hash, regardless of map iteration order.
func Compute(in Input) Fingerprint {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s", in.Method, in.Path, canonicalHeaders(in.Headers))
	h.Write(in.CanonicalBody)

	return Fingerprint{Hash: "sha256:" + hex.EncodeToString(h.Sum(nil))}
}

// canonicalHeaders renders a header map deterministically: names sorted
// case-insensitively, each header's values joined and newline-separated.
func canonicalHeaders(headers map[string][]string) string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})

	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s:%s\n", strings.ToLower(name), strings.Join(headers[name], ","))
	}
	return b.String()
}
