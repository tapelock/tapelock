// Package assertion checks a recorded interaction against deterministic
// rules. v0.1 ships exactly three primitives: status, schema, usage — no
// DSL, no scoring, no LLM-as-judge.
package assertion

import (
	"context"

	"github.com/tapelock/tapelock/internal/cassette"
)

// Assertion checks one property of a recorded interaction. Check is a pure
// function of (interaction, the assertion's own configuration) — nothing
// in this package consults an LLM, the network, or wall-clock state to
// decide a verdict.
type Assertion interface {
	Check(ctx context.Context, it cassette.Interaction) error
}
