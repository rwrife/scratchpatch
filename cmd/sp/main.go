// Command sp is the scratchpatch CLI: git stash for the throwaway files you
// were never going to commit anyway.
package main

import (
	"fmt"
	"os"

	"github.com/rwrife/scratchpatch/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
