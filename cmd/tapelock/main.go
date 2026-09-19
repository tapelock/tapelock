// Command tapelock is a deterministic HTTP record/replay proxy for LLM APIs.
//
// It records real HTTP interactions to JSONL cassettes and replays them
// in tests and CI without making live API requests.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Exit codes used by the CLI.
const (
	exitOK            = 0
	exitAssertionFail = 1
	exitCassetteMiss  = 2
	exitConfigError   = 3
	exitInternalError = 4
)

func main() {
	os.Exit(run())
}

func run() int {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternalError
	}
	return exitOK
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

func newRecordCmd() *cobra.Command {
	var cassette, upstream string

	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record HTTP interactions to a cassette",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("record: not implemented yet")
		},
	}

	cmd.Flags().StringVar(&cassette, "cassette", "", "path to the cassette file")
	cmd.Flags().StringVar(&upstream, "upstream", "", "upstream LLM API URL")
	_ = cmd.MarkFlagRequired("cassette")
	_ = cmd.MarkFlagRequired("upstream")

	return cmd
}

func newReplayCmd() *cobra.Command {
	var cassette string

	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay recorded interactions from a cassette",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("replay: not implemented yet")
		},
	}

	cmd.Flags().StringVar(&cassette, "cassette", "", "path to the cassette file")
	_ = cmd.MarkFlagRequired("cassette")

	return cmd
}

func newCheckCmd() *cobra.Command {
	var cassette string

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate recorded interactions and run assertions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("check: not implemented yet")
		},
	}

	cmd.Flags().StringVar(&cassette, "cassette", "", "path to the cassette file")
	_ = cmd.MarkFlagRequired("cassette")

	return cmd
}
