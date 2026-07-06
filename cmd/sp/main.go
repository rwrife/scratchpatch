// Command sp is the scratchpatch CLI: git stash for the throwaway files you
// were never going to commit anyway.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/rwrife/scratchpatch/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		// `sp scan` signals "secrets found" with a message-less sentinel so it
		// can gate hooks/CI via exit code without a redundant stderr line — the
		// scan report already said everything. Exit non-zero, but stay quiet.
		if errors.Is(err, cli.ErrSecretsFound) {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
