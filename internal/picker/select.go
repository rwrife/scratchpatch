package picker

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// ErrCanceled is returned when the user backs out of the picker (Esc / Ctrl-C /
// EOF at the prompt, or an fzf that exited without a selection). It's a normal,
// no-op outcome — the caller should report it gently and change nothing, per
// scratchpatch's "cancelling is always safe" stance.
var ErrCanceled = fmt.Errorf("selection canceled")

// IO bundles the streams the interactive picker talks to. Threading these
// through (rather than reaching for os.Stdin/Stdout directly) keeps the prompt
// testable: a test drives it with buffers, real use wires it to the process
// stdio.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Options tunes how Select chooses a front-end. TTY reports whether the session
// is attached to a real terminal (the caller detects this); when false, Select
// skips both fzf and the interactive filter loop and uses the one-shot numbered
// prompt so piped/non-interactive use still degrades cleanly. AllowFzf lets the
// caller force the built-in picker even when fzf is installed (e.g. a future
// --no-fzf flag); it defaults to true via SelectDefaults.
type Options struct {
	TTY      bool
	AllowFzf bool
	// lookFzf resolves the fzf binary; overridable in tests. nil means "use
	// exec.LookPath". Unexported so the public surface stays small.
	lookFzf func() (string, bool)
	// runFzf runs fzf with the given candidate labels and returns the chosen
	// label; overridable in tests. nil means "spawn the real fzf".
	runFzf func(io IO, labels []string) (string, error)
}

// Select is the picker entry point `sp open` uses when given no id. It presents
// the candidates and returns the chosen scratch, or ErrCanceled if the user
// backs out. The front-end is chosen in priority order:
//
//  1. fzf, when it's installed, allowed, and we're on a TTY — the issue's
//     "detect and defer to fzf if present".
//  2. the built-in interactive filter prompt, on a TTY without fzf — a
//     keyboard-driven, fuzzy-filterable numbered list.
//  3. a one-shot numbered prompt, when not a TTY — the graceful degradation
//     required for pipes and dumb terminals.
//
// An empty candidate slice is a caller error (there's nothing to pick); Select
// returns ErrCanceled so `sp open` can print a friendly "no scratches" line.
func Select(streams IO, cands []Candidate, opts Options) (Candidate, error) {
	if len(cands) == 0 {
		return Candidate{}, ErrCanceled
	}

	look := opts.lookFzf
	if look == nil {
		look = lookPathFzf
	}

	if opts.TTY && opts.AllowFzf {
		if path, ok := look(); ok {
			run := opts.runFzf
			if run == nil {
				run = func(streams IO, labels []string) (string, error) {
					return runRealFzf(streams, path, labels)
				}
			}
			return selectViaFzf(streams, cands, run)
		}
	}

	if opts.TTY {
		return selectInteractive(streams, cands)
	}
	return selectNumbered(streams, cands)
}

// SelectDefaults returns Options wired for real use: fzf allowed, TTY-ness as
// detected by the caller. Kept separate from Select so tests can construct
// Options with fakes without going through defaulting.
func SelectDefaults(tty bool) Options {
	return Options{TTY: tty, AllowFzf: true}
}

// lookPathFzf reports whether an `fzf` binary is on PATH.
func lookPathFzf() (string, bool) {
	path, err := exec.LookPath("fzf")
	if err != nil {
		return "", false
	}
	return path, true
}

// selectViaFzf hands the candidate labels to fzf (through run), then maps the
// chosen line back to its Candidate. fzf exiting with no selection (the user hit
// Esc, or filtered to nothing and pressed enter) surfaces as ErrCanceled.
func selectViaFzf(streams IO, cands []Candidate, run func(IO, []string) (string, error)) (Candidate, error) {
	labels := make([]string, len(cands))
	byLabel := make(map[string]Candidate, len(cands))
	for i, c := range cands {
		labels[i] = c.Label
		byLabel[c.Label] = c
	}

	chosen, err := run(streams, labels)
	if err != nil {
		return Candidate{}, err
	}
	chosen = strings.TrimRight(chosen, "\r\n")
	if chosen == "" {
		return Candidate{}, ErrCanceled
	}
	c, ok := byLabel[chosen]
	if !ok {
		// fzf returned something we didn't offer (shouldn't happen); treat it
		// as a cancel rather than opening the wrong thing.
		return Candidate{}, ErrCanceled
	}
	return c, nil
}

// runRealFzf spawns fzf, feeding it the labels on stdin and reading the picked
// line from stdout. fzf draws its own UI on the terminal via /dev/tty, so we
// leave the process's stderr attached for that. A non-zero exit with no output
// (code 130 = Esc/Ctrl-C, 1 = no match selected) is a cancel, not an error.
func runRealFzf(streams IO, path string, labels []string) (string, error) {
	cmd := exec.Command(path,
		"--prompt", "open scratch> ",
		"--height", "40%",
		"--reverse",
		"--no-multi",
	)
	cmd.Stdin = strings.NewReader(strings.Join(labels, "\n") + "\n")
	cmd.Stderr = streams.Err

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// fzf's documented cancel codes: 130 (interrupt) and 1 (no match).
			if code := exitErr.ExitCode(); code == 130 || code == 1 {
				return "", ErrCanceled
			}
		}
		return "", fmt.Errorf("run fzf: %w", err)
	}
	return string(out), nil
}

// selectInteractive runs the built-in, dependency-free picker on a TTY: it
// prints the numbered candidate list, then reads lines from the user. A number
// picks that row; any other text is treated as a fuzzy query that re-filters and
// re-prints the list. An empty line accepts the current top candidate, a bare
// "q" or EOF cancels. This gives the "keyboard-driven filter/fuzzy match" the
// issue asks for without putting the terminal into raw mode.
func selectInteractive(streams IO, cands []Candidate) (Candidate, error) {
	reader := bufio.NewReader(streams.In)
	filtered := cands

	fmt.Fprintln(streams.Out, "pick a scratch to open — type to filter, a number to choose, q to cancel:")
	printList(streams.Out, filtered)

	for {
		fmt.Fprint(streams.Out, "> ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			// EOF or read error with nothing typed: treat as cancel.
			fmt.Fprintln(streams.Out)
			return Candidate{}, ErrCanceled
		}
		input := strings.TrimSpace(line)

		switch {
		case input == "q":
			return Candidate{}, ErrCanceled
		case input == "":
			// Accept the current best (top of the filtered list).
			if len(filtered) == 0 {
				fmt.Fprintln(streams.Out, "nothing matches — refine your filter or q to cancel.")
				continue
			}
			return filtered[0], nil
		}

		// A pure number in range selects that row directly.
		if n, perr := strconv.Atoi(input); perr == nil {
			if n >= 1 && n <= len(filtered) {
				return filtered[n-1], nil
			}
			fmt.Fprintf(streams.Out, "pick a number between 1 and %d, or type to filter.\n", len(filtered))
			continue
		}

		// Otherwise treat it as a fuzzy query and re-render.
		filtered = Filter(cands, input)
		if len(filtered) == 0 {
			fmt.Fprintf(streams.Out, "no scratch matches %q — try fewer characters, or q to cancel.\n", input)
			continue
		}
		printList(streams.Out, filtered)
	}
}

// selectNumbered is the non-TTY degradation: it prints the numbered list once
// and reads a single line. A valid number picks that row; anything else (or
// EOF) cancels. No re-filtering loop, because without a terminal there's no
// interactive session to speak of — this is the "degrades to a numbered prompt"
// path for pipes and scripts that still want to choose.
func selectNumbered(streams IO, cands []Candidate) (Candidate, error) {
	printList(streams.Out, cands)
	fmt.Fprintf(streams.Out, "choose 1-%d (anything else cancels): ", len(cands))

	reader := bufio.NewReader(streams.In)
	line, _ := reader.ReadString('\n')
	input := strings.TrimSpace(line)
	if input == "" {
		return Candidate{}, ErrCanceled
	}
	n, err := strconv.Atoi(input)
	if err != nil || n < 1 || n > len(cands) {
		return Candidate{}, ErrCanceled
	}
	return cands[n-1], nil
}

// printList writes the numbered candidate labels to w. It's the one place the
// row numbering is rendered, so the interactive and numbered front-ends stay
// consistent. An empty list prints nothing (callers handle the "no matches"
// message with more context).
func printList(w io.Writer, cands []Candidate) {
	for i, c := range cands {
		fmt.Fprintf(w, "  %2d. %s\n", i+1, c.Label)
	}
}
