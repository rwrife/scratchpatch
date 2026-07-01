package schedule

import (
	"encoding/xml"
	"os/exec"
	"strings"
	"testing"
)

func testPlan() Plan {
	return Plan{BinPath: "/usr/local/bin/sp", Args: []string{"reap"}}
}

func TestCronLineHasScheduleCommandAndMarker(t *testing.T) {
	line := testPlan().CronLine()

	if !strings.HasPrefix(line, DefaultSchedule+" ") {
		t.Errorf("cron line should start with the schedule %q; got %q", DefaultSchedule, line)
	}
	if !strings.Contains(line, "reap") {
		t.Errorf("cron line should invoke reap; got %q", line)
	}
	if !strings.HasSuffix(line, Marker) {
		t.Errorf("cron line should end with the idempotency marker %q; got %q", Marker, line)
	}
	// The binary path must be present and quoted so a spaced path survives.
	if !strings.Contains(line, "'/usr/local/bin/sp'") {
		t.Errorf("cron line should contain the quoted binary path; got %q", line)
	}
}

func TestInstallOneLinerIsIdempotentByMarker(t *testing.T) {
	one := testPlan().InstallOneLiner()

	// It must strip any prior scratchpatch line (grep -v on the marker) before
	// appending, which is what guarantees no duplicate on re-run.
	if !strings.Contains(one, "grep -v '"+Marker+"'") {
		t.Errorf("installer should filter existing lines by marker; got %q", one)
	}
	// It must both read the current crontab and load a new one.
	if !strings.Contains(one, "crontab -l") || !strings.Contains(one, "crontab -") {
		t.Errorf("installer should read and reload the crontab; got %q", one)
	}
	// Tolerate the first-time "no crontab" case.
	if !strings.Contains(one, "2>/dev/null") {
		t.Errorf("installer should swallow the empty-crontab error; got %q", one)
	}
	// The meaningful invariant: composing the installer against an empty crontab
	// must reproduce CronLine() byte-for-byte (the cron line is embedded as a
	// shell literal, so a substring check would be brittle against escaping).
	composed, err := composeInstalledLine(t, one)
	if err != nil {
		t.Fatalf("running installer under sh: %v", err)
	}
	if composed != testPlan().CronLine() {
		t.Errorf("installed line = %q, want %q", composed, testPlan().CronLine())
	}
}

func TestUninstallOneLinerStripsByMarker(t *testing.T) {
	one := testPlan().UninstallOneLiner()
	if !strings.Contains(one, "grep -v '"+Marker+"'") {
		t.Errorf("uninstaller should filter by marker; got %q", one)
	}
	if strings.Contains(one, DefaultSchedule) {
		t.Errorf("uninstaller should not re-add the schedule line; got %q", one)
	}
}

func TestLaunchdPlistIsWellFormedAndScheduled(t *testing.T) {
	p := testPlan().LaunchdPlist()

	// It must parse as XML — the strongest guard that we emitted a valid plist.
	var anything interface{}
	if err := xml.Unmarshal([]byte(p), &anything); err != nil {
		t.Fatalf("launchd plist is not well-formed XML: %v\n%s", err, p)
	}
	for _, want := range []string{
		LaunchdLabel,
		"<key>ProgramArguments</key>",
		"/usr/local/bin/sp",
		"<string>reap</string>",
		"<key>StartCalendarInterval</key>",
		"<key>Hour</key><integer>3</integer>",
		"<key>Minute</key><integer>0</integer>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("launchd plist missing %q\n%s", want, p)
		}
	}
	// We deliberately don't RunAtLoad — no surprise reap at login.
	if strings.Contains(p, "RunAtLoad") {
		t.Errorf("launchd plist should not set RunAtLoad; got:\n%s", p)
	}
}

func TestLaunchdPlistNameMatchesLabel(t *testing.T) {
	if got, want := LaunchdPlistName(), LaunchdLabel+".plist"; got != want {
		t.Errorf("plist name = %q, want %q", got, want)
	}
}

func TestShellQuoteEscapesSpacesAndQuotes(t *testing.T) {
	p := Plan{BinPath: "/Applications/Scratch Patch/sp", Args: []string{"reap"}}
	line := p.CronLine()
	// A spaced path must be single-quoted as one argument.
	if !strings.Contains(line, "'/Applications/Scratch Patch/sp'") {
		t.Errorf("spaced path should be quoted whole; got %q", line)
	}

	p2 := Plan{BinPath: "/opt/o'dd/sp", Args: []string{"reap"}}
	line2 := p2.CronLine()
	// An embedded single quote must be escaped in the POSIX '\'' style.
	if !strings.Contains(line2, `'\''`) {
		t.Errorf("embedded single quote should be escaped; got %q", line2)
	}
}

// The installer must embed the cron line as a shell literal so that a binary
// path containing spaces round-trips byte-for-byte through the `echo ... |
// crontab -` pipeline. This is the regression guard for the naive-quoting bug
// where nested single quotes silently split a spaced path into two words.
func TestInstallOneLinerSurvivesSpacedPath(t *testing.T) {
	p := Plan{BinPath: "/Applications/Scratch Patch/sp", Args: []string{"reap"}}
	one := p.InstallOneLiner()
	composed, err := composeInstalledLine(t, one)
	if err != nil {
		t.Fatalf("running installer under sh: %v", err)
	}
	if composed != p.CronLine() {
		t.Errorf("installed crontab line drifted from CronLine()\n got: %q\nwant: %q", composed, p.CronLine())
	}
}

// composeInstalledLine executes the install one-liner under /bin/sh with
// `crontab -` swapped for `cat`, capturing exactly what would have been written
// to the crontab. It starts from an empty crontab (crontab -l redirected to
// /dev/null in the one-liner already tolerates that), so the sole output line is
// the freshly-appended entry.
func composeInstalledLine(t *testing.T, oneLiner string) (string, error) {
	t.Helper()
	// Replace only the terminal loader so nothing touches a real crontab.
	script := strings.Replace(oneLiner, "| crontab -", "| cat", 1)
	out, err := exec.Command("/bin/sh", "-c", script).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func TestLaunchdPlistEscapesXMLSpecials(t *testing.T) {
	p := Plan{BinPath: "/opt/a&b/sp", Args: []string{"reap"}}
	out := p.LaunchdPlist()
	if strings.Contains(out, "a&b") {
		t.Errorf("raw ampersand should have been escaped; got:\n%s", out)
	}
	if !strings.Contains(out, "a&amp;b") {
		t.Errorf("ampersand should be XML-escaped to &amp;; got:\n%s", out)
	}
	// And it must still be valid XML with the escape in place.
	var anything interface{}
	if err := xml.Unmarshal([]byte(out), &anything); err != nil {
		t.Fatalf("escaped plist is not well-formed XML: %v\n%s", err, out)
	}
}
