package cli

import (
	"strings"
	"testing"

	"github.com/rwrife/scratchpatch/internal/schedule"
)

// The scheduler flags on `sp reap` only ever print setup text — they must never
// touch the store. These tests drive them through the real root command and
// assert both the generated content and that nothing was reaped.

func TestReapInstallCronPrintsIdempotentLine(t *testing.T) {
	s := newSession(t)

	// Seed an expired scratch: if --install-cron accidentally ran a real reap,
	// this would get swept to the morgue. It must stay live.
	id := s.newScratchID("bystander")
	s.expire(id)

	out, err := s.run("reap", "--install-cron", "--no-color")
	if err != nil {
		t.Fatalf("reap --install-cron: %v (out=%s)", err, out)
	}

	if !strings.Contains(out, schedule.Marker) {
		t.Errorf("install-cron output should contain the idempotency marker %q; got:\n%s", schedule.Marker, out)
	}
	if !strings.Contains(out, schedule.DefaultSchedule) {
		t.Errorf("install-cron output should contain the cron schedule %q; got:\n%s", schedule.DefaultSchedule, out)
	}
	if !strings.Contains(out, "crontab -") {
		t.Errorf("install-cron output should include the idempotent installer; got:\n%s", out)
	}
	if !strings.Contains(out, "reap") {
		t.Errorf("install-cron output should schedule a reap; got:\n%s", out)
	}

	// The bystander must NOT have been swept — scheduler flags change nothing.
	if !s.indexHas(id) {
		t.Fatal("install-cron must not run a real reap")
	}
	live, _ := s.run("ls")
	if !strings.Contains(live, id) {
		t.Errorf("scratch %s should still be live after --install-cron; got:\n%s", id, live)
	}
}

func TestReapLaunchdEmitsPlist(t *testing.T) {
	s := newSession(t)

	out, err := s.run("reap", "--install-cron", "--launchd", "--no-color")
	if err != nil {
		t.Fatalf("reap --install-cron --launchd: %v (out=%s)", err, out)
	}
	for _, want := range []string{
		"<plist",
		schedule.LaunchdLabel,
		schedule.LaunchdPlistName(),
		"launchctl load",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("launchd output missing %q; got:\n%s", want, out)
		}
	}
}

func TestReapUninstallCronPrintsRemoval(t *testing.T) {
	s := newSession(t)

	out, err := s.run("reap", "--uninstall-cron", "--no-color")
	if err != nil {
		t.Fatalf("reap --uninstall-cron: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "grep -v") || !strings.Contains(out, schedule.Marker) {
		t.Errorf("uninstall output should strip the marker line; got:\n%s", out)
	}
	// It should not re-add the schedule.
	if strings.Contains(out, schedule.DefaultSchedule) {
		t.Errorf("uninstall output should not contain the schedule spec; got:\n%s", out)
	}
}

func TestReapScheduleFlagConflicts(t *testing.T) {
	s := newSession(t)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"install+uninstall", []string{"reap", "--install-cron", "--uninstall-cron"}, "opposites"},
		{"launchd alone", []string{"reap", "--launchd"}, "only applies with --install-cron"},
		{"dry-run+install", []string{"reap", "--dry-run", "--install-cron"}, "no meaning"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := s.run(tc.args...)
			if err == nil {
				t.Fatalf("expected error for %v; got out=%q", tc.args, out)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error for %v = %q, want it to contain %q", tc.args, err.Error(), tc.want)
			}
		})
	}
}

// Sanity: a plain reap still works alongside the new flags (no regression).
func TestReapStillReapsWithoutScheduleFlags(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("doomed")
	s.expire(id)

	out, err := s.run("reap")
	if err != nil {
		t.Fatalf("reap: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, id) {
		t.Errorf("plain reap should still sweep %s; got:\n%s", id, out)
	}
	live, _ := s.run("ls")
	if strings.Contains(live, id) {
		t.Errorf("swept scratch %s should no longer be live", id)
	}
}
