// Package proxy is a thin net/http adaptation layer in front of the engine.
// It never decides whether to record or replay, how to hash a request, or
// how a cassette is stored — it only translates http.Request/ResponseWriter
// to and from Handler calls. Streaming responses (SSE) are forwarded as raw
// bytes, chunk by chunk, never re-parsed or re-serialized (see mvp.md §3;
// full streaming support lands with the J10 milestone).
package proxy

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// Handler is the shape engine.Engine and engine.ReplayEngine both satisfy.
// Proxy depends on this interface rather than importing the engine
// package, so it never has an opinion on how a request is recorded,
// replayed, or hashed.
type Handler interface {
	Handle(ctx context.Context, req *http.Request) (*http.Response, error)
}

// missError is implemented by errors representing a deterministic cassette
// miss (engine.MissError), as opposed to a genuine upstream/network
// failure. Checking for it through this small interface, rather than
// importing the engine package, is what keeps Proxy from having any
// opinion on how replay works.
type missError interface {
	error
	CassetteMiss() bool
}

// MissHeader is set to "1" on the response when Handler.Handle failed with
// a deterministic cassette miss, so a caller can distinguish "nothing
// recorded matches this request" from any other proxy error without
// parsing the response body.
const MissHeader = "X-Tapelock-Miss"

// Proxy adapts a Handler to net/http.
type Proxy struct {
	Handler Handler

	// ErrorLog receives request-handling errors. It defaults to log.Printf
	// if nil.
	ErrorLog func(format string, args ...any)
}

// hopByHopResponseHeaders are recomputed from the buffered response rather
// than copied from upstream: the upstream's framing (chunked, a specific
// Content-Length) does not necessarily match how Proxy re-serves an
// already-fully-read body.
var hopByHopResponseHeaders = []string{"Content-Length", "Transfer-Encoding"}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp, err := p.Handler.Handle(r.Context(), r)
	if err != nil {
		p.logf("tapelock: %v", err)

		var me missError
		if errors.As(err, &me) && me.CassetteMiss() {
			w.Header().Set(MissHeader, "1")
		}
		http.Error(w, "tapelock: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	dst := w.Header()
	for name, values := range resp.Header {
		if isHopByHopResponseHeader(name) {
			continue
		}
		for _, v := range values {
			dst.Add(name, v)
		}
	}
	if resp.ContentLength >= 0 {
		dst.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}

	w.WriteHeader(resp.StatusCode)

	// A manual read-write-flush loop instead of io.Copy: flushing after
	// every write is what makes a streamed response (SSE) reach the client
	// as bytes arrive rather than sitting in a buffer. Doing this
	// unconditionally, rather than only when Handler happens to return a
	// stream, is what lets Proxy stay ignorant of whether this response is
	// one — a stream and a small buffered body are written the same way.
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				p.logf("tapelock: write response to client: %v", writeErr)
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				p.logf("tapelock: read response body: %v", readErr)
			}
			return
		}
	}
}

func isHopByHopResponseHeader(name string) bool {
	for _, h := range hopByHopResponseHeaders {
		if strings.EqualFold(h, name) {
			return true
		}
	}
	return false
}

func (p *Proxy) logf(format string, args ...any) {
	if p.ErrorLog != nil {
		p.ErrorLog(format, args...)
		return
	}
	log.Printf(format, args...)
}
