// Package schedule builds the scheduler snippets that make `sp reap` run on its
// own — without scratchpatch ever becoming a resident daemon.
//
// The design rule from PLAN.md is explicit: "no daemon or background service —
// reaping is on-demand or via the cron snippet, never a resident process." So
// this package deliberately does not spawn, watch, or supervise anything. It
// only *generates text*: a crontab line for Linux/BSD/macOS and a launchd
// property list for macOS users who prefer it. Installing that text is the
// user's deliberate act (a copy-paste, or the printed one-liner), which keeps
// the "no surprises, nothing runs behind your back" contract of the tool.
//
// Everything here is a pure function of a Plan, so it is trivially testable and
// has no side effects — the CLI layer decides whether to print or write.
package schedule

import (
	"fmt"
	"strings"
)

// Marker is the sentinel comment appended to the generated crontab line. It is
// what makes install/uninstall idempotent: tooling (and our own printed
// one-liner) can grep for this exact string to find "the scratchpatch line" and
// avoid adding a second one, or to strip it back out. It must stay stable
// across versions, so treat it as a compatibility surface.
const Marker = "# scratchpatch:auto-reap"

// LaunchdLabel is the reverse-DNS label for the macOS launchd job. launchd keys
// jobs by label, so reusing the same one means loading twice replaces rather
// than duplicates — the launchd equivalent of the cron Marker's idempotency.
const LaunchdLabel = "com.scratchpatch.reap"

// DefaultSchedule is the crontab time spec for the generated line: 03:00 every
// day. Reaping is cheap and idempotent, so a daily off-peak sweep is a sane
// default that keeps the morgue from lingering without being noisy.
const DefaultSchedule = "0 3 * * *"

// launchdHour / launchdMinute mirror DefaultSchedule for the launchd
// StartCalendarInterval, which takes numeric fields rather than a cron spec.
const (
	launchdHour   = 3
	launchdMinute = 0
)

// Plan is the resolved input to snippet generation: the absolute path to the
// `sp` binary and the arguments that constitute a reap run. Passing the binary
// path explicitly (rather than assuming "sp" is on cron's PATH) matters because
// cron and launchd run with a minimal environment where the login shell's PATH
// is absent — a bare "sp" would simply not be found.
type Plan struct {
	// BinPath is the absolute path to the sp executable to invoke.
	BinPath string
	// Args are the arguments appended after BinPath (e.g. []string{"reap"}).
	// Kept configurable so a future flag could schedule, say, `reap --by-size`
	// without this package needing to know about it.
	Args []string
}

// command renders the "run sp with these args" fragment shared by both the cron
// line and the launchd argument vector, quoting the binary path so a space in
// it (e.g. an install under "Application Support") survives the shell that cron
// uses to run the line.
func (p Plan) command() string {
	parts := append([]string{shellQuote(p.BinPath)}, p.Args...)
	return strings.Join(parts, " ")
}

// CronLine returns the single crontab entry that runs the reap on DefaultSchedule,
// suffixed with Marker so it can be found and removed idempotently. It is a bare
// line with no trailing newline, so callers can place it in prose or a block as
// they see fit.
func (p Plan) CronLine() string {
	return fmt.Sprintf("%s %s %s", DefaultSchedule, p.command(), Marker)
}

// InstallOneLiner returns a shell command the user can paste to install the cron
// line idempotently: it filters any existing scratchpatch line out of the
// current crontab, appends a fresh one, and reloads. Because the filter keys on
// Marker, running it twice leaves exactly one line — satisfying the
// "re-running install doesn't create duplicates" requirement even though we
// never touch the crontab ourselves.
func (p Plan) InstallOneLiner() string {
	// `crontab -l` exits non-zero when there is no crontab yet; the redirect to
	// /dev/null keeps the pipeline from aborting on that first-time case. grep
	// -v strips any prior scratchpatch line; then we append the current one and
	// load the combined result from stdin.
	//
	// Both the marker and the whole cron line are embedded as *shell literals*
	// (shellQuote) rather than by wrapping them in raw quotes here. That matters
	// because CronLine already contains a single-quoted binary path; naively
	// putting it inside echo '...' would let those inner quotes terminate the
	// echo string and mangle a spaced path. shellQuote produces a single,
	// correctly-escaped literal so the emitted crontab line is byte-for-byte
	// CronLine(), spaces and all.
	return fmt.Sprintf(
		"( crontab -l 2>/dev/null | grep -v %s ; echo %s ) | crontab -",
		shellQuote(Marker), shellQuote(p.CronLine()),
	)
}

// UninstallOneLiner returns the mirror of InstallOneLiner: strip the
// scratchpatch line and reload, removing the schedule cleanly. It is a no-op if
// no line is present, so it is always safe to run. The marker is passed as a
// shell literal for the same robustness reason as the installer.
func (p Plan) UninstallOneLiner() string {
	return fmt.Sprintf(
		"crontab -l 2>/dev/null | grep -v %s | crontab -",
		shellQuote(Marker),
	)
}

// LaunchdPlist returns a complete launchd property list that runs the reap daily
// at the same time as the cron default. It is the macOS-native alternative for
// users who would rather manage a LaunchAgent than a crontab. The label is
// LaunchdLabel so `launchctl load`-ing it twice replaces the job rather than
// duplicating it.
func (p Plan) LaunchdPlist() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString("<dict>\n")
	b.WriteString("  <key>Label</key>\n")
	fmt.Fprintf(&b, "  <string>%s</string>\n", LaunchdLabel)
	b.WriteString("  <key>ProgramArguments</key>\n")
	b.WriteString("  <array>\n")
	fmt.Fprintf(&b, "    <string>%s</string>\n", xmlEscape(p.BinPath))
	for _, a := range p.Args {
		fmt.Fprintf(&b, "    <string>%s</string>\n", xmlEscape(a))
	}
	b.WriteString("  </array>\n")
	b.WriteString("  <key>StartCalendarInterval</key>\n")
	b.WriteString("  <dict>\n")
	fmt.Fprintf(&b, "    <key>Hour</key><integer>%d</integer>\n", launchdHour)
	fmt.Fprintf(&b, "    <key>Minute</key><integer>%d</integer>\n", launchdMinute)
	b.WriteString("  </dict>\n")
	// RunAtLoad is intentionally omitted: we don't want a reap to fire the
	// instant the agent is loaded, only on the schedule. Nothing about reaping
	// is time-critical enough to justify a surprise run at login.
	b.WriteString("</dict>\n")
	b.WriteString("</plist>\n")
	return b.String()
}

// LaunchdPlistName is the conventional filename for the LaunchAgent plist,
// derived from the label. Callers surface it so the user knows where to drop the
// file (~/Library/LaunchAgents/<name>).
func LaunchdPlistName() string {
	return LaunchdLabel + ".plist"
}

// shellQuote wraps s in single quotes for safe use in the /bin/sh command line
// cron builds, escaping any embedded single quotes. Paths are the realistic
// input, so this is conservative rather than a full shell-quoting library.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// xmlEscape escapes the handful of characters that are significant inside a
// plist <string>. Binary paths won't usually contain these, but a path with an
// ampersand shouldn't produce a malformed plist.
func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}
