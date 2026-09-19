// Package fingerprint turns a sanitized, canonicalized request into a stable
// SHA-256 key: sanitize -> JCS canonicalization -> hash. This is Tapelock's
// core differentiator, so it stays pure (no I/O) and is exercised with
// table-driven and fuzz tests rather than integration tests.
package fingerprint
