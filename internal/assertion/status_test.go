package assertion

import (
	"context"
	"testing"

	"github.com/tapelock/tapelock/internal/cassette"
)

func TestStatusAssertionCheck(t *testing.T) {
	tests := []struct {
		name    string
		want    int
		status  int
		wantErr bool
	}{
		{name: "match", want: 200, status: 200, wantErr: false},
		{name: "mismatch", want: 200, status: 500, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := StatusAssertion{Want: tt.want}
			it := cassette.Interaction{Response: cassette.ResponseSnapshot{Status: tt.status}}

			err := a.Check(context.Background(), it)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
