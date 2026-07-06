package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFixture loads a testdata file or fails the test.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// kinds collects the set of Kinds present in a finding slice.
func kinds(fs []Finding) map[Kind]bool {
	m := map[Kind]bool{}
	for _, f := range fs {
		m[f.Kind] = true
	}
	return m
}

func TestScanCleanFileTripsNothing(t *testing.T) {
	got := Scan(readFixture(t, "clean.txt"))
	if len(got) != 0 {
		t.Fatalf("clean file should trip nothing; got %d findings: %+v", len(got), got)
	}
	if Tripped(readFixture(t, "clean.txt")) {
		t.Error("Tripped should be false for a clean file")
	}
}

func TestScanPlaceholdersStayQuiet(t *testing.T) {
	// A .env.example full of template values must not cry wolf — that's the
	// whole point of alarm-fatigue avoidance.
	got := Scan(readFixture(t, "placeholders.txt"))
	if len(got) != 0 {
		t.Fatalf("placeholder template should trip nothing; got %+v", got)
	}
}

func TestScanDotenvCatchesTheRealSecrets(t *testing.T) {
	content := readFixture(t, "dotenv.txt")
	got := Scan(content)
	if len(got) == 0 {
		t.Fatal("dotenv dump should trip the detector")
	}

	ks := kinds(got)
	if !ks[KindAWSAccessKey] {
		t.Error("expected the AWS access key id to be caught")
	}
	if !ks[KindAssignment] {
		t.Error("expected secret-looking assignments to be caught")
	}

	// The plain-prose NOTES= line must not be flagged.
	for _, f := range got {
		if f.Line == 7 {
			t.Errorf("prose line 7 should not trip; got %+v", f)
		}
	}
}

func TestScanFindsPrivateKeyHeader(t *testing.T) {
	got := Scan(readFixture(t, "private_key.txt"))
	if !kinds(got)[KindPrivateKey] {
		t.Fatalf("expected a private-key finding; got %+v", got)
	}
	// The header sits on line 1.
	var found bool
	for _, f := range got {
		if f.Kind == KindPrivateKey {
			found = true
			if f.Line != 1 {
				t.Errorf("private key header should be line 1; got line %d", f.Line)
			}
		}
	}
	if !found {
		t.Error("private-key finding missing")
	}
}

func TestScanFindsBareBearerTokenViaEntropy(t *testing.T) {
	got := Scan(readFixture(t, "bearer.txt"))
	if !kinds(got)[KindHighEntropy] && !kinds(got)[KindAssignment] {
		t.Fatalf("expected the bearer token to trip high-entropy (or assignment); got %+v", got)
	}
}

func TestFindingsNeverContainRawSecret(t *testing.T) {
	// The cardinal rule: no finding may echo a full secret value. Check that
	// the known raw secrets from the fixtures never appear verbatim in any
	// Masked field.
	rawSecrets := []string{
		"AKIAIOSFODNN7EXAMPLE",
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"EXAMPLExQzLkdIwqZ9mNbVcXsWePqRtYuIoP",
		"EXAMPLEwWPw5k4aXcaT4fNP0UcnZwJUVFk6LO0p1nJx",
		"9f8Kd2LmQx7RvT1nZ4pW6sYb3cJhAeUgN5oXiE0",
	}
	for _, fx := range []string{"dotenv.txt", "bearer.txt", "private_key.txt"} {
		for _, f := range Scan(readFixture(t, fx)) {
			for _, raw := range rawSecrets {
				if strings.Contains(f.Masked, raw) {
					t.Errorf("%s: masked value leaked a raw secret %q in %+v", fx, raw, f)
				}
			}
		}
	}
}

func TestMaskRedactsMiddleAndStarsShortValues(t *testing.T) {
	// Long value: keep 3 + 3, redact the middle.
	got := Mask("AKIAIOSFODNN7EXAMPLE")
	if strings.Contains(got, "IOSFODNN") {
		t.Errorf("mask must hide the middle; got %q", got)
	}
	if !strings.HasPrefix(got, "AKI") || !strings.HasSuffix(got, "PLE") {
		t.Errorf("mask should keep first/last 3; got %q", got)
	}

	// Short value: fully starred, no characters revealed.
	short := Mask("abcd")
	if short != "****" {
		t.Errorf("short value should be fully starred; got %q", short)
	}
	if Mask("") != "" {
		t.Errorf("empty mask should stay empty; got %q", Mask(""))
	}
}

func TestScanReportsAscendingLineNumbers(t *testing.T) {
	got := Scan(readFixture(t, "dotenv.txt"))
	last := 0
	for _, f := range got {
		if f.Line < last {
			t.Errorf("findings should be in ascending line order; %d after %d", f.Line, last)
		}
		if f.Line < 1 {
			t.Errorf("line numbers are 1-based; got %d", f.Line)
		}
		last = f.Line
	}
}

func TestScanIgnoresPlainNumbersAndProse(t *testing.T) {
	// Long digit runs (ids, timestamps) and ordinary sentences must not trip
	// the high-entropy fallback.
	cases := []string{
		"the build finished in 1234567890123456 nanoseconds",
		"order id 000000001111112222223333334444445555556666",
		"This is a perfectly ordinary sentence with several longish words in it.",
		"see the docs at https://example.com/very/long/path/that/is/just/a/url/here",
	}
	for _, c := range cases {
		if got := Scan([]byte(c)); len(got) != 0 {
			t.Errorf("expected no findings for %q; got %+v", c, got)
		}
	}
}

func TestScanDedupesPerKindPerLine(t *testing.T) {
	// Two AWS keys on one line should yield a single aws-access-key finding for
	// that line (we don't pile on).
	line := "keys: AKIAIOSFODNN7EXAMPLE and AKIAJEXAMPLE1234567X"
	var awsCount int
	for _, f := range Scan([]byte(line)) {
		if f.Kind == KindAWSAccessKey {
			awsCount++
		}
	}
	if awsCount != 1 {
		t.Errorf("expected one aws finding per line, got %d", awsCount)
	}
}
