package assertion

import (
	"context"
	"fmt"

	"github.com/tapelock/tapelock/internal/cassette"
)

// StatusAssertion checks the response's HTTP status code against a single
// expected value.
type StatusAssertion struct {
	Want int
}

func (a StatusAssertion) Check(ctx context.Context, it cassette.Interaction) error {
	if it.Response.Status != a.Want {
		return fmt.Errorf("assertion: status: got %d, want %d", it.Response.Status, a.Want)
	}
	return nil
}
