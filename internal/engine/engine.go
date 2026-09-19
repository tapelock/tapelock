// Package engine orchestrates record/replay behavior. It knows nothing about
// http.Server, files on disk, or the CLI — it depends only on the ports
// (Fingerprinter, CassetteStore, Upstream) implemented by the other internal
// packages. This is what lets 90% of Tapelock's behavior be unit-tested
// without ever starting the proxy.
package engine
