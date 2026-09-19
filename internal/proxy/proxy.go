// Package proxy is a thin net/http adaptation layer in front of the engine.
// It never decides whether to record or replay, how to hash a request, or
// how a cassette is stored — it only translates http.Request/ResponseWriter
// to and from engine calls. Streaming responses (SSE) are forwarded as raw
// bytes, chunk by chunk, never re-parsed or re-serialized.
package proxy
