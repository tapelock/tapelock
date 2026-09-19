// Command tapelock is a deterministic HTTP record/replay proxy for LLM APIs.
//
// It records real HTTP interactions to JSONL cassettes and replays them
// in tests and CI without making live API requests.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tapelock/tapelock/internal/assertion"
	"github.com/tapelock/tapelock/internal/cassette"
	"github.com/tapelock/tapelock/internal/engine"
	"github.com/tapelock/tapelock/internal/proxy"
	"github.com/tapelock/tapelock/internal/sanitize"
)

// Exit codes used by the CLI.
const (
	exitOK            = 0
	exitAssertionFail = 1
	exitCassetteMiss  = 2
	exitConfigError   = 3
	exitInternalError = 4
)

// exitCoder is implemented by errors that carry a specific process exit
// code. A plain error that doesn't implement it exits with
// exitInternalError.
type exitCoder interface {
	error
	ExitCode() int
}

// cliError pairs an error with the exit code it should produce, without
// forcing every call site to know about os.Exit.
type cliError struct {
	code int
	err  error
}

func (e *cliError) Error() string { return e.err.Error() }
func (e *cliError) Unwrap() error { return e.err }
func (e *cliError) ExitCode() int { return e.code }

func configErrorf(format string, args ...any) error {
	return &cliError{code: exitConfigError, err: fmt.Errorf(format, args...)}
}

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	err := root.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tapelock:", err)
	}
	return exitCodeFor(err)
}

// exitCodeFor maps an error returned by root.ExecuteContext to a process
// exit code. Every error our own RunE functions return is wrapped as a
// cliError with an explicit code, so a plain error reaching here is almost
// always cobra's own flag/usage validation (missing required flag, unknown
// flag, bad flag value), a configuration error, not an unexpected
// internal failure.
func exitCodeFor(err error) int {
	if err == nil {
		return exitOK
	}
	if ec, ok := errors.AsType[exitCoder](err); ok {
		return ec.ExitCode()
	}
	return exitConfigError
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "tapelock",
		Short: "Deterministic HTTP record/replay proxy for LLM APIs",
	}

	root.AddCommand(newRecordCmd())
	root.AddCommand(newReplayCmd())
	root.AddCommand(newCheckCmd())

	return root
}

// defaultSanitizer is used by both record and replay: they must sanitize
// identically, or a request recorded under one hash would never replay
// (see internal/engine's fingerprintRequest).
func defaultSanitizer() *sanitize.Sanitizer {
	return sanitize.New(sanitize.UUID, sanitize.Timestamp)
}

func newRecordCmd() *cobra.Command {
	var cassettePath, upstreamURL, listenAddr string

	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record HTTP interactions to a cassette",
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := url.Parse(upstreamURL)
			if err != nil || base.Scheme == "" || base.Host == "" {
				return configErrorf("invalid --upstream %q", upstreamURL)
			}

			store, err := cassette.NewManager(cassettePath)
			if err != nil {
				return configErrorf("cassette: %v", err)
			}

			eng := &engine.Engine{
				Upstream:  proxy.NewUpstream(base, nil),
				Store:     store,
				Sanitizer: defaultSanitizer(),
			}

			return serve(cmd.Context(), cmd.OutOrStdout(), listenAddr, &proxy.Proxy{Handler: eng}, "record")
		},
	}

	cmd.Flags().StringVar(&cassettePath, "cassette", "", "path to the cassette file")
	cmd.Flags().StringVar(&upstreamURL, "upstream", "", "upstream LLM API URL")
	cmd.Flags().StringVar(&listenAddr, "listen", "127.0.0.1:0", "address to listen on")
	_ = cmd.MarkFlagRequired("cassette")
	_ = cmd.MarkFlagRequired("upstream")

	return cmd
}

func newReplayCmd() *cobra.Command {
	var cassettePath, listenAddr string

	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay recorded interactions from a cassette",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := cassette.NewManager(cassettePath)
			if err != nil {
				return configErrorf("cassette: %v", err)
			}

			re := &engine.ReplayEngine{Store: store, Sanitizer: defaultSanitizer()}

			var missed atomic.Bool
			handler := &missTrackingHandler{Handler: re, missed: &missed}

			if err := serve(cmd.Context(), cmd.OutOrStdout(), listenAddr, &proxy.Proxy{Handler: handler}, "replay"); err != nil {
				return err
			}
			if missed.Load() {
				return &cliError{code: exitCassetteMiss, err: errors.New("one or more requests missed the cassette during this run")}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cassettePath, "cassette", "", "path to the cassette file")
	cmd.Flags().StringVar(&listenAddr, "listen", "127.0.0.1:0", "address to listen on")
	_ = cmd.MarkFlagRequired("cassette")

	return cmd
}

// missTrackingHandler records whether any request handled during the
// process's lifetime resulted in a cassette miss, so `replay`'s exit code
// reflects it even though a single miss only fails that one HTTP response,
// not the whole (long-running) process.
type missTrackingHandler struct {
	proxy.Handler
	missed *atomic.Bool
}

func (h *missTrackingHandler) Handle(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := h.Handler.Handle(ctx, req)
	if _, ok := errors.AsType[*engine.MissError](err); ok {
		h.missed.Store(true)
	}
	return resp, err
}

// serve binds addr, announces the actual bound address on out, and runs
// handler until ctx is canceled (SIGINT/SIGTERM) or the server itself
// fails. A canceled context is a clean shutdown, never an error.
func serve(ctx context.Context, out io.Writer, addr string, handler http.Handler, name string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// A listen failure is almost always a bad or already-in-use
		// --listen address, i.e. a configuration problem, not an
		// unexpected internal one.
		return configErrorf("listen on %s: %v", addr, err)
	}

	fmt.Fprintf(out, "tapelock %s listening on http://%s\n", name, ln.Addr())

	srv := &http.Server{Handler: handler}
	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		<-serveErr
		return nil
	case err := <-serveErr:
		// An unexpected failure of the server itself (not a bad --listen
		// address, already handled above) is the one genuinely internal
		// error case in this CLI.
		return &cliError{code: exitInternalError, err: fmt.Errorf("serve: %w", err)}
	}
}

func newCheckCmd() *cobra.Command {
	var cassettePath, schemaPath string
	var wantStatus, maxPromptTokens, maxCompletionTokens, maxTotalTokens int

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate recorded interactions and run assertions",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := cassette.NewManager(cassettePath)
			if err != nil {
				return configErrorf("cassette: %v", err)
			}

			var assertions []assertion.Assertion
			if cmd.Flags().Changed("status") {
				assertions = append(assertions, assertion.StatusAssertion{Want: wantStatus})
			}
			if schemaPath != "" {
				schemaJSON, err := os.ReadFile(schemaPath)
				if err != nil {
					return configErrorf("read --schema: %v", err)
				}
				sa, err := assertion.NewSchemaAssertion(schemaJSON)
				if err != nil {
					return configErrorf("--schema: %v", err)
				}
				assertions = append(assertions, sa)
			}
			if maxPromptTokens > 0 || maxCompletionTokens > 0 || maxTotalTokens > 0 {
				assertions = append(assertions, assertion.UsageAssertion{
					MaxPromptTokens:     maxPromptTokens,
					MaxCompletionTokens: maxCompletionTokens,
					MaxTotalTokens:      maxTotalTokens,
				})
			}

			out := cmd.OutOrStdout()
			interactions := store.Read()
			fmt.Fprintf(out, "tapelock check: %d interaction(s) in %s\n", len(interactions), cassettePath)

			failures := 0
			for _, it := range interactions {
				for _, a := range assertions {
					if err := a.Check(cmd.Context(), it); err != nil {
						failures++
						fmt.Fprintf(out, "  FAIL %s: %v\n", it.ID, err)
					}
				}
			}

			if failures > 0 {
				return &cliError{code: exitAssertionFail, err: fmt.Errorf("%d assertion failure(s)", failures)}
			}
			fmt.Fprintln(out, "tapelock check: ok")
			return nil
		},
	}

	cmd.Flags().StringVar(&cassettePath, "cassette", "", "path to the cassette file")
	cmd.Flags().IntVar(&wantStatus, "status", 0, "expected HTTP status code for every interaction")
	cmd.Flags().StringVar(&schemaPath, "schema", "", "path to a JSON Schema file to validate response bodies against")
	cmd.Flags().IntVar(&maxPromptTokens, "max-prompt-tokens", 0, "fail if usage.prompt_tokens exceeds this")
	cmd.Flags().IntVar(&maxCompletionTokens, "max-completion-tokens", 0, "fail if usage.completion_tokens exceeds this")
	cmd.Flags().IntVar(&maxTotalTokens, "max-total-tokens", 0, "fail if usage.total_tokens exceeds this")
	_ = cmd.MarkFlagRequired("cassette")

	return cmd
}
