// Package sanitize replaces volatile values (UUIDs, timestamps, user-defined
// regex matches) in a copy of the request body before it reaches the
// fingerprint hasher. It never touches the request actually sent upstream.
package sanitize
