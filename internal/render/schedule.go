// Schedule report rendering: the human-facing framing for `sp reap
// --install-cron` and friends.
//
// Like the rest of render, this is the only layer that decides on color. The
// schedule package produces the actual snippet text (cron line, install
// one-liner, launchd plist); render just wraps it with a headline and
// copy-paste guidance, tinting the headline on a TTY. The snippet body itself is
// never colored, so a user redirecting `sp reap --install-cron > setup.sh`
// still gets clean, runnable text.
package render

import (
	"fmt"
	"io"
	"strings"
)

// ScheduleKind selects which flavor of scheduler guidance to render, so the
// single ScheduleReport entry point can serve install, uninstall, and the
// launchd alternative without three near-identical functions in the CLI.
type ScheduleKind int

const (
	// ScheduleInstallCron renders the crontab line plus the idempotent
	// install one-liner and instructions.
	ScheduleInstallCron ScheduleKind = iota
	// ScheduleUninstallCron renders the removal one-liner and instructions.
	ScheduleUninstallCron
	// ScheduleLaunchd renders the macOS launchd plist plus load/unload steps.
	ScheduleLaunchd
)

// ScheduleReportData is the plain data ScheduleReport needs. The CLI fills it
// from the schedule package so render imports neither schedule nor anything
// about the environment.
type ScheduleReportData struct {
	// Kind selects the guidance flavor.
	Kind ScheduleKind
	// CronLine is the bare crontab entry (install only).
	CronLine string
	// InstallOneLiner / UninstallOneLiner are paste-ready shell commands.
	InstallOneLiner   string
	UninstallOneLiner string
	// Plist is the full launchd property list body (launchd only).
	Plist string
	// PlistName is the conventional filename for the plist (launchd only).
	PlistName string
}

// ScheduleReport writes scheduler setup guidance to w. It never changes the
// user's system — it only prints what to run — which keeps `sp` true to its
// "no daemon, nothing happens behind your back" contract.
func ScheduleReport(w io.Writer, d ScheduleReportData, color bool) error {
	pal := defaultPalette()
	var b strings.Builder

	switch d.Kind {
	case ScheduleInstallCron:
		writeLine(&b, "schedule reap — a daily cron sweep, no daemon involved", color, pal.header)
		b.WriteString("\nAdd this line to your crontab (runs `sp reap` at 03:00 daily):\n\n")
		fmt.Fprintf(&b, "  %s\n", d.CronLine)
		b.WriteString("\nOr install it idempotently (re-running replaces, never duplicates):\n\n")
		fmt.Fprintf(&b, "  %s\n", d.InstallOneLiner)
		b.WriteString("\nRemove it later with `sp reap --uninstall-cron`.\n")

	case ScheduleUninstallCron:
		writeLine(&b, "unschedule reap — remove the cron sweep", color, pal.header)
		b.WriteString("\nRun this to strip the scratchpatch line from your crontab (safe if none exists):\n\n")
		fmt.Fprintf(&b, "  %s\n", d.UninstallOneLiner)

	case ScheduleLaunchd:
		writeLine(&b, "schedule reap — macOS launchd agent (cron alternative)", color, pal.header)
		fmt.Fprintf(&b, "\nSave this as ~/Library/LaunchAgents/%s :\n\n", d.PlistName)
		// The plist body is emitted verbatim and uncolored so redirecting the
		// output to the file produces a valid property list.
		b.WriteString(d.Plist)
		b.WriteString("\nThen load it (re-loading replaces the existing agent):\n\n")
		fmt.Fprintf(&b, "  launchctl unload ~/Library/LaunchAgents/%s 2>/dev/null; \\\n", d.PlistName)
		fmt.Fprintf(&b, "  launchctl load ~/Library/LaunchAgents/%s\n", d.PlistName)
		b.WriteString("\nUnschedule later by unloading and deleting that file.\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}
