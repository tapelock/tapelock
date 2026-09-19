package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tapelock/tapelock/internal/cassette"
	"github.com/tapelock/tapelock/internal/sanitize"
)

// CassetteLookup is what ReplayEngine needs from a cassette.
// cassette.Manager already implements it via Manager.Lookup.
type CassetteLookup interface {
	Lookup(hash string) (cassette.Interaction, bool)
}

// MissError is returned when no recorded interaction matches a request's
// Fingerprint. There is no fallback to a live API call: a miss is always an
// error, which is what makes replay deterministic (see mvp.md §9).
type MissError struct {
	Method string
	Path   string
	Hash   string
}

func (e *MissError) Error() string {
	return fmt.Sprintf("tapelock: cassette miss for %s %s (%s)", e.Method, e.Path, e.Hash)
}

// CassetteMiss reports true, marking MissError as a deterministic "nothing
// recorded matches this request" case rather than a generic failure. See
// proxy.Proxy, which checks for it through this method (via an unexported
// interface) instead of importing this package.
func (e *MissError) CassetteMiss() bool { return true }

// ReplayEngine serves recorded responses from a cassette. It never makes a
// network call: a request whose Fingerprint isn't in the cassette is a
// *MissError, never a fallback to the real API.
type ReplayEngine struct {
	Store CassetteLookup

	// Sanitizer must match whatever recorded the cassette being replayed —
	// see fingerprintRequest. A mismatch doesn't corrupt anything, it just
	// makes every request a miss.
	Sanitizer *sanitize.Sanitizer
}

// Handle computes the same Fingerprint Engine.Handle would have recorded
// this request under and serves that interaction's response verbatim.
func (e *ReplayEngine) Handle(ctx context.Context, req *http.Request) (*http.Response, error) {
	reqBody, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("replay: read request body: %w", err)
	}
	req.Body.Close()

	fp, err := fingerprintRequest(e.Sanitizer, req, reqBody)
	if err != nil {
		return nil, fmt.Errorf("replay: fingerprint request: %w", err)
	}

	it, ok := e.Store.Lookup(fp.Hash)
	if !ok {
		return nil, &MissError{Method: req.Method, Path: req.URL.Path, Hash: fp.Hash}
	}

	if it.Response.Stream {
		// Replay timing is always "instant" in v0.1: chunk boundaries and
		// content are preserved exactly, but there is no artificial delay
		// between them — reproducing the original network timing is a
		// config option (stream_timing: recorded|scaled) for later.
		var body bytes.Buffer
		for _, chunk := range it.Response.Chunks {
			body.WriteString(chunk.Data)
		}
		return &http.Response{
			StatusCode:    it.Response.Status,
			Header:        http.Header(it.Response.Headers),
			Body:          io.NopCloser(bytes.NewReader(body.Bytes())),
			ContentLength: int64(body.Len()),
		}, nil
	}

	return &http.Response{
		StatusCode:    it.Response.Status,
		Header:        http.Header(it.Response.Headers),
		Body:          io.NopCloser(strings.NewReader(it.Response.Body)),
		ContentLength: int64(len(it.Response.Body)),
	}, nil
}
