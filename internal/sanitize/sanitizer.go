// Package sanitize replaces volatile values (UUIDs, timestamps, user-defined
// regex matches) in a copy of the request body before it reaches the
// fingerprint hasher. It never touches the request actually sent upstream.
package sanitize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

// Rule replaces every match of Pattern inside a JSON string value with
// Replace. A Rule only ever touches string leaf values — object keys and
// the JSON structure itself are left alone.
type Rule struct {
	Pattern *regexp.Regexp
	Replace string
}

// Built-in rules, referenced by name from tapelock.yaml (e.g.
// `sanitize: - name: uuid`).
var (
	// UUID matches the canonical 8-4-4-4-12 hyphenated form.
	UUID = Rule{
		Pattern: regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`),
		Replace: "<UUID>",
	}

	// Timestamp matches RFC 3339 / ISO 8601 date-times, with or without
	// fractional seconds.
	Timestamp = Rule{
		Pattern: regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`),
		Replace: "<TIMESTAMP>",
	}
)

// NewRule builds a custom sanitization rule from a regular expression and
// its replacement.
func NewRule(pattern, replace string) (Rule, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return Rule{}, fmt.Errorf("sanitize: invalid pattern %q: %w", pattern, err)
	}
	return Rule{Pattern: re, Replace: replace}, nil
}

// Sanitizer applies an ordered list of rules to every string value in a
// JSON document.
type Sanitizer struct {
	rules []Rule
}

// New builds a Sanitizer that applies rules in order.
func New(rules ...Rule) *Sanitizer {
	return &Sanitizer{rules: append([]Rule(nil), rules...)}
}

// Sanitize decodes body as JSON, applies every rule to each string value,
// and re-encodes the result. body is never modified — the caller's
// original request bytes are what get sent upstream; only the returned
// bytes are safe to hash.
func (s *Sanitizer) Sanitize(body []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // preserve exact numeric text; avoid float64 round-tripping

	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("sanitize: decode: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // placeholders like "<UUID>" must stay literal
	if err := enc.Encode(s.walk(value)); err != nil {
		return nil, fmt.Errorf("sanitize: encode: %w", err)
	}

	// json.Encoder.Encode always appends a trailing newline; EncodeLine and
	// Canonicalize both expect a bare JSON value.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func (s *Sanitizer) walk(value any) any {
	switch v := value.(type) {
	case string:
		return s.apply(v)
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = s.walk(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = s.walk(e)
		}
		return out
	default:
		// json.Number, bool, nil: left untouched.
		return value
	}
}

func (s *Sanitizer) apply(str string) string {
	for _, r := range s.rules {
		str = r.Pattern.ReplaceAllString(str, r.Replace)
	}
	return str
}
