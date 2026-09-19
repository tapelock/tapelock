package cassette

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// CurrentVersion is the JSONL cassette format version this package reads
// and writes. There is no cross-version migration in v0.1: any other
// version on a line is a hard error.
const CurrentVersion = 1

// BodyEncoding says how a snapshot's Body is encoded on disk. The empty
// value means Body is stored as-is (valid UTF-8, typically JSON) rather
// than guessing at the content on every read.
type BodyEncoding string

// Supported body encodings.
const (
	BodyEncodingIdentity BodyEncoding = ""
	BodyEncodingBase64   BodyEncoding = "base64"
)

// Headers holds HTTP header values as they were sent or received, keyed by
// header name with one or more values (mirrors net/http.Header without
// importing net/http into a dependency-free domain package).
type Headers map[string][]string

// Interaction is one recorded request/response pair: the unit of a
// cassette. It is encoded as exactly one line in a cassette file, so it
// diffs cleanly in code review.
type Interaction struct {
	Version int    `json:"version"`
	ID      string `json:"id"`

	Request     RequestSnapshot  `json:"request"`
	RequestHash string           `json:"request_hash"`
	Response    ResponseSnapshot `json:"response"`
}

// RequestSnapshot is the request as it was sent to the upstream, before any
// sanitization. Sanitization only ever affects what gets hashed
// (RequestHash); it never changes what is stored here.
type RequestSnapshot struct {
	Method  string  `json:"method"`
	URL     string  `json:"url"`
	Headers Headers `json:"headers,omitempty"`

	Body         string       `json:"body,omitempty"`
	BodyEncoding BodyEncoding `json:"body_encoding,omitempty"`
}

// ResponseSnapshot is the response returned to the client. A non-streaming
// response sets Body; a streaming response sets Stream and Chunks instead
// and leaves Body empty.
type ResponseSnapshot struct {
	Status  int     `json:"status"`
	Headers Headers `json:"headers,omitempty"`

	Body         string       `json:"body,omitempty"`
	BodyEncoding BodyEncoding `json:"body_encoding,omitempty"`

	Stream bool            `json:"stream,omitempty"`
	Chunks []ResponseChunk `json:"chunks,omitempty"`
}

// ResponseChunk is one SSE frame captured during recording, stored as the
// exact bytes and order it arrived in. DelayMS is the time elapsed since
// the previous chunk (or since the response started, for the first chunk).
type ResponseChunk struct {
	Data    string `json:"data"`
	DelayMS int64  `json:"delay_ms"`
}

// EncodeLine renders an interaction as a single JSONL line, without a
// trailing newline. Encoding the same value always produces the same
// bytes: encoding/json sorts map keys and struct fields encode in
// declaration order, so two cassettes recorded from identical interactions
// diff as empty.
func EncodeLine(it Interaction) ([]byte, error) {
	if it.Version != CurrentVersion {
		return nil, fmt.Errorf("cassette: unsupported interaction version %d (want %d)", it.Version, CurrentVersion)
	}
	return json.Marshal(it)
}

// DecodeLine parses a single JSONL cassette line into an Interaction. It
// rejects any version other than CurrentVersion and any field not defined
// above — an unrecognized field is treated as a corrupt or foreign cassette
// rather than silently ignored.
func DecodeLine(line []byte) (Interaction, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()

	var it Interaction
	if err := dec.Decode(&it); err != nil {
		return Interaction{}, fmt.Errorf("cassette: decode line: %w", err)
	}
	if it.Version != CurrentVersion {
		return Interaction{}, fmt.Errorf("cassette: unsupported interaction version %d (want %d)", it.Version, CurrentVersion)
	}
	return it, nil
}
