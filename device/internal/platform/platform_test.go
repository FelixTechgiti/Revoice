package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// mkroot builds a fake filesystem root. Files are given as path → contents;
// an empty content string makes a directory instead, which is what
// /dev/__properties__ is on a real device.
func mkroot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, content := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if content == "" {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The os-release emOS actually writes, from emos/build.sh. Kept verbatim so
// this test fails if that format changes shape.
const emosRelease = `NAME="emOS"
ID=emos
PRETTY_NAME="emOS 0.1.3"
VERSION="0.1.3"
VERSION_ID="0.1.3"
BUILD_ID="20260904T071342Z"
`

func TestDetect(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "emOS: os-release stamped at build time",
			files: map[string]string{"etc/os-release": emosRelease},
			want:  EmOS,
		},
		{
			name:  "FireOS: Android's property service",
			files: map[string]string{"dev/__properties__": ""},
			want:  FireOS,
		},
		{
			// Neither marker. Must NOT guess: a wrong answer here sends a
			// device payloads it cannot use.
			name:  "neither marker present",
			files: map[string]string{},
			want:  Unknown,
		},
		{
			// Some other distribution's os-release must not read as emOS.
			name:  "os-release from something else",
			files: map[string]string{"etc/os-release": "NAME=\"Debian\"\nID=debian\n"},
			want:  Unknown,
		},
		{
			// ID=emos wins over the property service. They are mutually
			// exclusive on real hardware, so this only fires if something has
			// gone strange — and claiming emOS is the safe half, since the
			// consequence is skipping Android payloads rather than pushing
			// Android payloads at a device with no package manager.
			name: "both markers: emOS wins",
			files: map[string]string{
				"etc/os-release":     emosRelease,
				"dev/__properties__": "",
			},
			want: EmOS,
		},
		{
			// The ID must be the whole value, not a prefix match — an
			// "emosaic" distribution is not ours.
			name:  "ID that merely starts with emos",
			files: map[string]string{"etc/os-release": "ID=emosaic\n"},
			want:  Unknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(mkroot(t, tc.files)); got != tc.want {
				t.Errorf("Detect() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Base caches, and the value must be stable for the life of the process: a
// field that flapped would read as a device changing platform underneath the
// controller.
func TestBaseIsStable(t *testing.T) {
	first := Base()
	for i := 0; i < 3; i++ {
		if got := Base(); got != first {
			t.Fatalf("Base() returned %q then %q", first, got)
		}
	}
	switch first {
	case EmOS, FireOS, Unknown:
	default:
		t.Errorf("Base() = %q, not one of the three defined answers", first)
	}
}

// ── The emOS version (#255) ──────────────────────────────────────────────────

func writeOSRelease(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(root+"/etc", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/etc/os-release", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestVersionReadsTheStampedEmOSVersion(t *testing.T) {
	root := writeOSRelease(t, "NAME=\"emOS\"\nID=emos\nVERSION_ID=0.7.0-fx.1\nBUILD_ID=x\n")
	if got := Version(root); got != "0.7.0-fx.1" {
		t.Errorf("Version = %q, want 0.7.0-fx.1", got)
	}
}

// os-release permits quoting and emOS does not use it today for this field —
// which is exactly the kind of thing that changes without anyone noticing that
// a comparison downstream stopped matching.
func TestVersionStripsQuotes(t *testing.T) {
	root := writeOSRelease(t, "ID=emos\nVERSION_ID=\"0.7.0-fx.1\"\n")
	if got := Version(root); got != "0.7.0-fx.1" {
		t.Errorf("Version = %q, want the unquoted value", got)
	}
}

// Amazon's /system is mounted under BOTH bases, so an os-release that is not
// ours must not be reported as an emOS version — the controller would then
// offer a reflash against a release it has nothing to do with.
func TestVersionRefusesSomebodyElsesOSRelease(t *testing.T) {
	root := writeOSRelease(t, "NAME=\"Fire OS\"\nID=android\nVERSION_ID=5.5.5.4\n")
	if got := Version(root); got != "" {
		t.Errorf("Version = %q for a non-emOS os-release, want empty", got)
	}
}

// Empty rather than a placeholder, in all three of the ways it can fail. The
// controller compares this against a published release, so a string it cannot
// parse is worse than none: "unknown" would sort, compare and display as if it
// were a version.
func TestVersionIsEmptyRatherThanGuessed(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"no VERSION_ID at all", "ID=emos\nNAME=\"emOS\"\n"},
		{"empty VERSION_ID", "ID=emos\nVERSION_ID=\n"},
	} {
		if got := Version(writeOSRelease(t, tc.body)); got != "" {
			t.Errorf("%s: Version = %q, want empty", tc.name, got)
		}
	}
	if got := Version(t.TempDir()); got != "" {
		t.Errorf("no os-release at all: Version = %q, want empty", got)
	}
}

// Detect and Version read the same file and must agree about it, or the
// controller gets a version for a device it has been told is FireOS.
func TestDetectAndVersionAgree(t *testing.T) {
	root := writeOSRelease(t, "ID=emos\nVERSION_ID=0.7.0-fx.1\n")
	if Detect(root) != EmOS {
		t.Fatal("Detect did not recognise the fixture as emOS")
	}
	if Version(root) == "" {
		t.Error("Detect says emOS and Version says nothing")
	}
}
