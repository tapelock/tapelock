// Package fingerprint turns a sanitized, canonicalized request into a stable
// SHA-256 key: sanitize -> JCS canonicalization -> hash. This is Tapelock's
// core differentiator, so it stays pure (no I/O) and is exercised with
// table-driven and fuzz tests rather than integration tests.
package fingerprint

// Fingerprint identifies a semantically equivalent request. Two requests
// that differ only in fields sanitize.Sanitizer replaces (UUIDs,
// timestamps, custom regexes) or in JSON member order produce the same
// Fingerprint.
//
// The hashing itself (sanitize -> canonicalize -> SHA-256) is not
// implemented yet; this type only fixes the shape other packages depend on.
type Fingerprint struct {
	// Hash is "sha256:<hex>", computed over the canonical request.
	Hash string

	// Occurrence is the 0-based count of prior requests in the same
	// recording session that produced this same Hash. It lets agent loops
	// that repeat an identical request replay a different response each
	// time (see cassette.Interaction.RequestHash, which stores Hash alone —
	// occurrence is tracked by the caller, e.g. the cassette manager).
	Occurrence int
}
