package nxmtp

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The macOS app is a separate language with its own copies of a few facts that
// originate here. Nothing at build time connects them: Swift cannot import Go
// constants, and the FFI carries values rather than definitions. So a change on
// this side compiles, ships, and breaks the app silently.
//
// These tests read the Swift sources as text and assert the two sides still
// agree. They are deliberately in the Go package that owns the definition,
// because that is where a change starts. They also need no toolchain beyond
// file reading, so they run on the Linux CI job that has no Xcode.
//
// This is a guard, not an architecture. The real fix is for the app to consume
// these from the backend; until then, this makes divergence loud.

func readSwift(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		// A moved or renamed Swift file must fail the test rather than skip it.
		// A silently skipped contract test is worse than no test, because it
		// reports success while checking nothing.
		t.Fatalf("cannot read %s: %v\n"+
			"If the Swift sources moved, update this test — do not delete it.", path, err)
	}
	return string(b)
}

// TestInstallableExtensionsMatchTheSwiftApp pins the set of file types DBI's
// install storages accept. Swift declares its own copy in SwitchWorkflows.swift
// and uses it to decide which dropped files are installable; if the two sets
// drift, the app and the CLI disagree about what can be installed, and a user
// dragging in a newly supported format gets an unhelpful "cannot be installed"
// from one front-end and success from the other.
func TestInstallableExtensionsMatchTheSwiftApp(t *testing.T) {
	src := readSwift(t, "../../app/SwitchMTP/Services/SwitchWorkflows.swift")

	re := regexp.MustCompile(`installableExtensions:\s*Set<String>\s*=\s*\[([^\]]*)\]`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("could not find `installableExtensions: Set<String> = [...]` in " +
			"SwitchWorkflows.swift; if it was renamed or restructured, update this test")
	}

	var swift []string
	for _, raw := range strings.Split(m[1], ",") {
		s := strings.TrimSpace(raw)
		s = strings.Trim(s, `"`)
		if s == "" {
			continue
		}
		// Swift stores bare extensions ("nsp"); Go stores them dotted (".nsp").
		// Normalise so the comparison is about the set, not the spelling.
		swift = append(swift, "."+strings.ToLower(s))
	}

	got := append([]string(nil), swift...)
	want := append([]string(nil), InstallableExtensions...)
	sort.Strings(got)
	sort.Strings(want)

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("installable extensions have drifted between Go and Swift\n"+
			"  Go   (nxmtp.InstallableExtensions):        %v\n"+
			"  Swift (SwitchWorkflows.installableExtensions): %v\n"+
			"Update both, and the user-facing strings that list the formats.",
			want, got)
	}
}

// TestCancelledKindIsWhatTheSwiftAppMatches guards a subtle coupling.
//
// The FFI sends the error kind and message as separate JSON fields, but
// MTPManager flattens them into one string ("<kind>: <message>") before
// anything downstream sees it. ErrorStringLocalizer.isTransferCancelledError
// then recovers the meaning by substring match. That works today only because
// the value of KindCancelled literally contains "cancelled".
//
// This matters more than a cosmetic mismatch: that predicate is what triggers
// disposing and reconnecting the MTP session after a user cancel, because the
// session is left with broken transaction IDs. Renaming this kind to something
// reasonable like "aborted" would compile, pass every other test, and leave the
// app wedged after the first cancelled transfer.
func TestCancelledKindIsWhatTheSwiftAppMatches(t *testing.T) {
	src := readSwift(t, "../../app/SwitchMTP/Services/ErrorStringLocalizer.swift")

	re := regexp.MustCompile(`(?s)func isTransferCancelledError\([^)]*\)\s*->\s*Bool\s*\{(.*?)\n    \}`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("could not find isTransferCancelledError in ErrorStringLocalizer.swift; " +
			"if it was renamed or restructured, update this test")
	}
	body := m[1]

	needles := regexp.MustCompile(`contains\("([^"]+)"\)`).FindAllStringSubmatch(body, -1)
	if len(needles) == 0 {
		t.Fatal("isTransferCancelledError no longer matches on substrings; " +
			"if it now switches on the error kind directly, this test can be deleted")
	}

	// The app sees "<kind>: <message>". Cancellation must be detectable from
	// the kind alone, since the message is not guaranteed to mention it.
	kind := strings.ToLower(string(KindCancelled))
	for _, n := range needles {
		if strings.Contains(kind, strings.ToLower(n[1])) {
			return
		}
	}

	var matched []string
	for _, n := range needles {
		matched = append(matched, n[1])
	}
	t.Errorf("KindCancelled is %q, which none of the Swift app's substrings match: %v\n"+
		"The app would stop recognising cancelled transfers, and would skip the "+
		"session reset that a cancel requires. Either keep the kind matchable or "+
		"update ErrorStringLocalizer.isTransferCancelledError.",
		KindCancelled, matched)
}
