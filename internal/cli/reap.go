package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/render"
	"github.com/rwrife/scratchpatch/internal/schedule"
	"github.com/rwrife/scratchpatch/internal/store"
)

func newReapCommand() *cobra.Command {
	var noColor bool
	var dryRun bool
	var installCron bool
	var uninstallCron bool
	var launchd bool

	cmd := &cobra.Command{
		Use:   "reap",
		Short: "Sweep expired scratches to the morgue and purge past-grace ones",
		Long: "Run the reaper. It does two things, in order, and never both to the\n" +
			"same scratch in one run:\n\n" +
			"  1. Expired live scratches are moved into the morgue (soft-deleted).\n" +
			"     Their grace clock starts now — they are NOT purged this run.\n" +
			"  2. Morgue scratches that have aged past the grace window (default\n" +
			"     3d) are hard-deleted for good. This is the only place\n" +
			"     scratchpatch ever destroys content.\n\n" +
			"Pass --dry-run to see exactly what would move and what would be\n" +
			"deleted without changing anything.\n\n" +
			"To make cleanup automatic, --install-cron prints a ready-to-use\n" +
			"crontab line (--launchd emits a macOS launchd agent instead), and\n" +
			"--uninstall-cron prints how to remove it. These only print setup\n" +
			"instructions — scratchpatch never edits your crontab or runs a daemon.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Scheduler flags short-circuit the actual reap: they emit setup
			// text and change nothing. Guard against nonsensical combinations
			// so the user gets a clear error instead of silently ignored flags.
			if err := checkScheduleFlags(dryRun, installCron, uninstallCron, launchd); err != nil {
				return err
			}
			switch {
			case installCron || launchd:
				return runScheduleInstall(cmd, launchd, noColor)
			case uninstallCron:
				return runScheduleUninstall(cmd, noColor)
			default:
				return runReap(cmd, dryRun, noColor)
			}
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be swept/purged without changing anything")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "force plain output even on a TTY")
	cmd.Flags().BoolVar(&installCron, "install-cron", false, "print a crontab line (and idempotent installer) that runs `sp reap` daily")
	cmd.Flags().BoolVar(&launchd, "launchd", false, "with --install-cron, emit a macOS launchd agent instead of a crontab line")
	cmd.Flags().BoolVar(&uninstallCron, "uninstall-cron", false, "print how to remove the scheduled reap")

	return cmd
}

// checkScheduleFlags rejects flag combinations that can't be honored together,
// keeping the RunE switch above unambiguous. --launchd is a modifier for the
// install path, so it needs one of install/uninstall to attach to, and install
// vs uninstall are opposites.
func checkScheduleFlags(dryRun, installCron, uninstallCron, launchd bool) error {
	if installCron && uninstallCron {
		return fmt.Errorf("--install-cron and --uninstall-cron are opposites; pick one")
	}
	if launchd && !installCron {
		return fmt.Errorf("--launchd only applies with --install-cron")
	}
	if dryRun && (installCron || uninstallCron || launchd) {
		return fmt.Errorf("--dry-run has no meaning for the scheduler flags (they never change anything)")
	}
	return nil
}

func runReap(cmd *cobra.Command, dryRun, noColor bool) error {
	st, err := store.Open()
	if err != nil {
		return err
	}

	plan, err := st.Reap(time.Now(), dryRun)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	color := !noColor && isTerminal(out)

	return render.ReapSummary(out, render.ReapResult{
		Swept:  plan.Morgued,
		Purged: plan.Purged,
		DryRun: plan.DryRun,
	}, color)
}

// schedulePlan builds the schedule.Plan for the current invocation: the absolute
// path to this very binary plus the `reap` args, so the generated cron/launchd
// entry re-runs exactly `sp reap`. Resolving os.Executable() (rather than
// assuming "sp" is on cron's minimal PATH) is what makes the scheduled job
// actually find the binary.
func schedulePlan() schedule.Plan {
	return schedule.Plan{BinPath: resolveSelfPath(), Args: []string{"reap"}}
}

// resolveSelfPath returns the absolute path to the running executable, falling
// back to a bare "sp" if the runtime can't tell us (an exotic platform, a
// deleted binary). The fallback keeps the printed snippet useful — the user can
// fix the path — rather than failing the command outright.
func resolveSelfPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "sp"
	}
	if abs, err := filepath.Abs(exe); err == nil {
		return abs
	}
	return exe
}

func runScheduleInstall(cmd *cobra.Command, launchd, noColor bool) error {
	plan := schedulePlan()
	out := cmd.OutOrStdout()
	color := !noColor && isTerminal(out)

	if launchd {
		return render.ScheduleReport(out, render.ScheduleReportData{
			Kind:      render.ScheduleLaunchd,
			Plist:     plan.LaunchdPlist(),
			PlistName: schedule.LaunchdPlistName(),
		}, color)
	}

	return render.ScheduleReport(out, render.ScheduleReportData{
		Kind:            render.ScheduleInstallCron,
		CronLine:        plan.CronLine(),
		InstallOneLiner: plan.InstallOneLiner(),
	}, color)
}

func runScheduleUninstall(cmd *cobra.Command, noColor bool) error {
	plan := schedulePlan()
	out := cmd.OutOrStdout()
	color := !noColor && isTerminal(out)

	return render.ScheduleReport(out, render.ScheduleReportData{
		Kind:              render.ScheduleUninstallCron,
		UninstallOneLiner: plan.UninstallOneLiner(),
	}, color)
}
