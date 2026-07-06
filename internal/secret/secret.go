// Package secret is scratchpatch's tripwire for leaked credentials.
//
// AI coding agents and tired humans dump API keys, .env dumps, and private
// keys into throwaway files without thinking. This package scans content for
// the *shapes* those secrets take and reports where they are — never what they
// are. It is deliberately conservative: the goal is to catch the obvious leaks
// (AWS keys, `*_API_KEY=` assignments, PEM private-key headers, long
// high-entropy tokens) without crying wolf on ordinary prose or code, because
// a detector that fires constantly gets muted and then it protects nothing.
//
// Everything here is pure and clock-free: Scan takes bytes and returns
// findings, so it is trivially unit-testable over fixture files. Like the rest
// of scratchpatch, this package knows nothing about color, terminals, or the
// CLI — it returns plain data and lets the caller decide how to phrase it. And
// per the issue's hard rule, a Finding never carries a full secret value: it
// carries a Masked preview (first/last few characters with the middle redacted)
// so a report can point at a leak without re-leaking it to the terminal or to
// logs.
package secret

import (
	"bufio"
	"bytes"
	"math"
	"regexp"
	"strings"
)

// Kind names the category of secret a Finding matched, so callers (and scripts,
// via `sp scan --json` later) can group or filter without parsing prose.
type Kind string

const (
	// KindAWSAccessKey is an AWS access key id (AKIA/ASIA + 16 base32 chars).
	KindAWSAccessKey Kind = "aws-access-key"
	// KindPrivateKey is a PEM private-key header line ("-----BEGIN ... PRIVATE KEY-----").
	KindPrivateKey Kind = "private-key"
	// KindAssignment is a key/token assignment whose *name* looks secret
	// (API_KEY, SECRET, TOKEN, PASSWORD…) and whose value is non-trivial.
	KindAssignment Kind = "assignment"
	// KindHighEntropy is a long, high-entropy token that doesn't match a more
	// specific rule but looks generated rather than written.
	KindHighEntropy Kind = "high-entropy"
)

// Finding is one place a scan tripped. It names what kind of secret shape
// matched, which 1-based line it sat on, and a masked preview safe to print —
// never the raw value. Rule is a short human label for the specific heuristic
// (e.g. "AWS access key id") used in reports.
type Finding struct {
	Kind Kind
	// Line is the 1-based line number the match sat on.
	Line int
	// Rule is a short human label for the matched heuristic.
	Rule string
	// Masked is a redacted preview of the offending token, safe to display.
	// It never contains the full secret value.
	Masked string
}

// awsAccessKeyRe matches an AWS access key id: the AKIA/ASIA/AGPA/AIDA-family
// prefix followed by exactly 16 uppercase base32 characters. Anchored on word
// boundaries so it won't fire mid-identifier.
var awsAccessKeyRe = regexp.MustCompile(`\b(A3T|AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}\b`)

// assignmentRe matches "<name> <sep> <value>" where sep is '=' or ':' — the
// shape of shell/.env assignments and many config lines. The name and value are
// captured so the caller can decide (via secretName / value heuristics) whether
// this is actually a secret rather than, say, `count = 3`. Quotes around the
// value are tolerated and stripped by the caller.
var assignmentRe = regexp.MustCompile(`(?i)([A-Za-z_][A-Za-z0-9_.-]*)\s*[:=]\s*["']?([^\s"']{6,})["']?`)

// secretNameRe recognizes assignment *names* that strongly imply a credential.
// Kept conservative on purpose: we want KEY/SECRET/TOKEN/PASSWORD-family names,
// not every variable that happens to contain "key" as a substring of a word.
var secretNameRe = regexp.MustCompile(`(?i)(^|[_.-])(api[_.-]?key|secret|token|password|passwd|access[_.-]?key|private[_.-]?key|client[_.-]?secret|auth|credential|session[_.-]?key)([_.-]|$)`)

// pemHeaderRe matches a PEM private-key banner line. We flag on the header
// alone (the body is just base64) so a pasted key trips even if truncated.
var pemHeaderRe = regexp.MustCompile(`-----BEGIN ([A-Z0-9 ]*)?PRIVATE KEY-----`)

// tokenRe pulls candidate standalone tokens out of a line for the high-entropy
// fallback: runs of base64url / hex characters of a generated length.
// Slashes and '+'/'=' are deliberately excluded so URL paths and query strings
// (which are naturally high-entropy across their segments) don't trip the
// fallback — a real base64 secret with those characters is almost always in an
// assignment and is caught by the assignment rule instead. Bare opaque tokens
// (bearer tokens, opaque API keys) live in the [A-Za-z0-9_-] alphabet, which is
// what we scan for here.
var tokenRe = regexp.MustCompile(`[A-Za-z0-9_-]{24,}`)

const (
	// minEntropyBits is the Shannon-entropy floor (bits per char) a bare token
	// must clear to count as high-entropy. English prose sits well below this;
	// random base64/hex sits above it. Tuned to avoid flagging ordinary words,
	// URLs, and hashes-of-nothing while catching generated secrets.
	minEntropyBits = 3.6
	// minTokenLen is the shortest bare token the high-entropy rule considers.
	// Short tokens can't carry enough entropy to be confidently a secret.
	minTokenLen = 24
	// maskKeep is how many leading and trailing characters a mask preserves.
	maskKeep = 3
)

// Scan reads content and returns every place it tripped a secret heuristic, in
// ascending line order. It is pure and allocation-light: no I/O, no clock, no
// globals mutated — the same bytes always yield the same findings, which is
// what makes the fixture tests meaningful. A line may produce more than one
// finding (e.g. an assignment whose value is also high-entropy) but each rule
// reports a line at most once to avoid piling on. Content that trips nothing
// yields a nil slice, so callers can treat len==0 as "clean".
func Scan(content []byte) []Finding {
	var findings []Finding

	sc := bufio.NewScanner(bytes.NewReader(content))
	// Allow long lines (minified JSON, base64 blobs) without the scanner's
	// default 64 KiB token limit aborting the scan.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		findings = append(findings, scanLine(text, line)...)
	}
	return findings
}

// scanLine applies every rule to a single line, deduping so no single line
// emits two findings of the same Kind. Rules are checked most-specific first so
// a matched AWS key or PEM header isn't also reported as a generic high-entropy
// token.
func scanLine(text string, line int) []Finding {
	var out []Finding
	seen := map[Kind]bool{}

	add := func(k Kind, rule, raw string) {
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, Finding{Kind: k, Line: line, Rule: rule, Masked: Mask(raw)})
	}

	// 1. PEM private-key header — unambiguous, check first.
	if m := pemHeaderRe.FindString(text); m != "" {
		add(KindPrivateKey, "PEM private key header", m)
	}

	// 2. AWS access key id — specific prefix + fixed length.
	if m := awsAccessKeyRe.FindString(text); m != "" {
		add(KindAWSAccessKey, "AWS access key id", m)
	}

	// 3. Secret-named assignment — the name implies a credential and the value
	//    is non-trivial. This is the workhorse for .env-style leaks.
	if name, val, ok := secretAssignment(text); ok {
		add(KindAssignment, "secret-looking assignment ("+name+")", val)
	}

	// 4. High-entropy fallback — a long, generated-looking token that none of
	//    the above claimed. Skipped if we already flagged this line, so we
	//    don't double-report the value of a secret assignment.
	if len(out) == 0 {
		if tok := highEntropyToken(text); tok != "" {
			add(KindHighEntropy, "high-entropy token", tok)
		}
	}

	return out
}

// secretAssignment reports whether text is an assignment whose name looks like
// a credential and whose value is substantive (not a placeholder). It returns
// the matched name and the raw value so the caller can mask the value.
func secretAssignment(text string) (name, value string, ok bool) {
	m := assignmentRe.FindStringSubmatch(text)
	if m == nil {
		return "", "", false
	}
	name, value = m[1], m[2]
	if !secretNameRe.MatchString(name) {
		return "", "", false
	}
	if isPlaceholder(value) {
		return "", "", false
	}
	return name, value, true
}

// isPlaceholder filters out obvious non-secret values so a template like
// `API_KEY=changeme` or `TOKEN=<your-token>` doesn't cry wolf. Conservative: we
// only skip values that are clearly stand-ins, not anything that merely looks
// low-entropy.
func isPlaceholder(v string) bool {
	lv := strings.ToLower(strings.Trim(v, "<>{}[]()\"'"))
	switch lv {
	case "changeme", "change-me", "your-key", "your_key", "yourkey",
		"your-token", "your_token", "yourtoken", "xxx", "xxxx", "todo",
		"example", "placeholder", "none", "null", "nil", "empty",
		"redacted", "secret", "password", "test", "dummy", "fixme":
		return true
	}
	// A value made only of the same repeated char (xxxx, ****, ....) is a mask,
	// not a secret.
	if len(lv) > 0 && strings.Count(lv, string(lv[0])) == len(lv) {
		return true
	}
	// Interpolations like ${FOO} / $(cmd) / {{ var }} are references, not
	// literal secrets.
	if strings.HasPrefix(v, "${") || strings.HasPrefix(v, "$(") || strings.HasPrefix(v, "{{") {
		return true
	}
	return false
}

// highEntropyToken returns the first long, high-entropy token on the line, or
// "" if none qualifies. It exists to catch generated secrets that don't carry a
// telltale prefix or assignment name (bare bearer tokens, opaque API keys). The
// entropy floor, length minimum, and a "looks generated" mixed-character check
// keep it off ordinary words, hex color codes, git SHAs, URLs, and paths.
func highEntropyToken(text string) string {
	for _, tok := range tokenRe.FindAllString(text, -1) {
		if len(tok) < minTokenLen {
			continue
		}
		// Skip tokens that are all one character class in a way that reads as
		// structured rather than secret (e.g. a run of digits).
		if isAllDigits(tok) {
			continue
		}
		// Generated secrets mix character classes; a long run of only lowercase
		// letters is almost always a word or a slug, not a credential. Requiring
		// a mix (upper+lower, or letters+digits) is the single biggest lever
		// against flagging prose and URL path segments.
		if !looksGenerated(tok) {
			continue
		}
		if shannonEntropy(tok) >= minEntropyBits {
			return tok
		}
	}
	return ""
}

// looksGenerated reports whether tok mixes character classes the way a random
// token does: it must contain at least two of {lowercase, uppercase, digit}.
// Single-class runs (all-lowercase words, all-uppercase constants) read as
// human-written and are left alone to keep the false-positive rate low.
func looksGenerated(tok string) bool {
	var lower, upper, digit bool
	for _, r := range tok {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	classes := 0
	for _, on := range []bool{lower, upper, digit} {
		if on {
			classes++
		}
	}
	return classes >= 2
}

// isAllDigits reports whether s is only ASCII digits (a long number is not a
// secret by our heuristics — think ids, timestamps, counts).
func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// shannonEntropy returns the Shannon entropy of s in bits per character. Random
// base64 approaches ~6 bits/char; English text sits near ~2. We use it as a
// cheap "does this look generated?" signal for the high-entropy fallback.
func shannonEntropy(s string) float64 {
	if s == "" {
		return 0
	}
	var counts [256]float64
	n := 0
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
		n++
	}
	var h float64
	for _, c := range counts {
		if c == 0 {
			continue
		}
		p := c / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}

// Mask redacts a secret value for display: it keeps the first and last maskKeep
// characters and replaces the middle with a fixed-width ellipsis, so a report
// can show *which* token tripped without revealing it. Short values are fully
// starred so we never expose most of a small secret by "previewing" it. Mask is
// exported because the CLI masks values it pulls straight from a line (e.g. the
// assignment value it already parsed) as well as ones inside Findings.
func Mask(raw string) string {
	if raw == "" {
		return ""
	}
	// For anything short enough that first+last would reveal most of it, star
	// the whole thing.
	if len(raw) <= 2*maskKeep+2 {
		return strings.Repeat("*", len(raw))
	}
	return raw[:maskKeep] + "…" + raw[len(raw)-maskKeep:]
}

// Tripped reports whether content contains any secret shape at all. It's the
// cheap gate the CLI uses for `sp ls` markers and the `sp promote` block, where
// only the yes/no matters and the individual findings don't.
func Tripped(content []byte) bool {
	return len(Scan(content)) > 0
}
