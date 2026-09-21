package endpoint

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wilbowes/EchoMuse/internal/platform"
)

type fakeInfo struct {
	dir  bool
	mode fs.FileMode
	size int64
}

func (f fakeInfo) Name() string       { return "gaishim.so" }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.dir }
func (f fakeInfo) Sys() any           { return nil }

func stub(t *testing.T, base string, info os.FileInfo, err error) {
	t.Helper()
	oldBase, oldStat := resolverBase, resolverStat
	resolverBase = func() string { return base }
	resolverStat = func(string) (os.FileInfo, error) { return info, err }
	t.Cleanup(func() {
		resolverBase, resolverStat = oldBase, oldStat
		logMu.Lock()
		logAny, logLast = false, ""
		logMu.Unlock()
	})
}

func present() os.FileInfo { return fakeInfo{mode: 0o644, size: 4824} }

// The gate is base_os, and UNKNOWN must read as FireOS: absence must never
// change what the existing fleet does. A device that reports nothing is a
// device running firmware older than the field, and those run under Android.
func TestNeededOnlyOnEmOS(t *testing.T) {
	for _, tc := range []struct {
		base string
		want bool
	}{
		{platform.EmOS, true},
		{platform.FireOS, false},
		{platform.Unknown, false},
		{"", false},
	} {
		stub(t, tc.base, present(), nil)
		if got := ResolverStatus().Needed; got != tc.want {
			t.Errorf("base %q: Needed = %v, want %v", tc.base, got, tc.want)
		}
	}
}

// A shared object is mapped, not executed. Requiring +x would refuse a
// perfectly good library that arrived 0644 — which is what a file copy gives.
func TestPresenceChecksReadableNotExecutable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		info    os.FileInfo
		err     error
		present bool
		reason  string
	}{
		{"missing", nil, errors.New("no such file"), false, "not_installed"},
		{"a directory", fakeInfo{dir: true, mode: fs.ModeDir | 0o755}, nil, false, "not_a_file"},
		{"unreadable", fakeInfo{mode: 0o000}, nil, false, "not_readable"},
		{"plain 0644", fakeInfo{mode: 0o644, size: 4824}, nil, true, ""},
		{"0755", fakeInfo{mode: 0o755, size: 4824}, nil, true, ""},
	} {
		stub(t, platform.EmOS, tc.info, tc.err)
		r := ResolverStatus()
		if r.Present != tc.present || r.Reason != tc.reason {
			t.Errorf("%s: Present=%v Reason=%q, want %v %q",
				tc.name, r.Present, r.Reason, tc.present, tc.reason)
		}
	}
}

func preload(env []string) (string, bool) {
	for _, kv := range env {
		if n, v, ok := strings.Cut(kv, "="); ok && n == PreloadVar {
			return v, true
		}
	}
	return "", false
}

// The two halves of the gate, each alone being enough to withhold the preload.
func TestEnvSetsPreloadOnlyWhenNeededAndPresent(t *testing.T) {
	for _, tc := range []struct {
		name string
		base string
		info os.FileInfo
		err  error
		want bool
	}{
		{"emOS with the shim", platform.EmOS, present(), nil, true},
		{"emOS without it", platform.EmOS, nil, errors.New("gone"), false},
		{"FireOS with the shim", platform.FireOS, present(), nil, false},
		{"unknown base with the shim", platform.Unknown, present(), nil, false},
	} {
		stub(t, tc.base, tc.info, tc.err)
		got, ok := preload(ResolverStatus().Env([]string{"PATH=/bin"}))
		if ok != tc.want {
			t.Errorf("%s: LD_PRELOAD set = %v, want %v", tc.name, ok, tc.want)
		}
		if ok && got != ShimPath {
			t.Errorf("%s: LD_PRELOAD = %q, want %q", tc.name, got, ShimPath)
		}
	}
}

func TestEnvKeepsTheRestOfTheEnvironment(t *testing.T) {
	stub(t, platform.EmOS, present(), nil)
	in := []string{"PATH=/bin", "EM_SHM_DIR=/dev/shm"}
	out := ResolverStatus().Env(in)
	for _, want := range in {
		found := false
		for _, kv := range out {
			if kv == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not survive", want)
		}
	}
}

// Nothing in this firmware sets LD_PRELOAD today. A preload that silently
// dropped somebody else's is only ever discovered from the far side.
func TestEnvExtendsAnExistingPreload(t *testing.T) {
	stub(t, platform.EmOS, present(), nil)
	out := ResolverStatus().Env([]string{"LD_PRELOAD=/data/local/bin/other.so"})
	got, _ := preload(out)
	if got != ShimPath+":/data/local/bin/other.so" {
		t.Errorf("LD_PRELOAD = %q, want ours first then theirs", got)
	}
	if n := len(out); n != 1 {
		t.Errorf("env grew to %d entries; LD_PRELOAD should have been edited in place", n)
	}
}

func TestEnvDoesNotAddItselfTwice(t *testing.T) {
	stub(t, platform.EmOS, present(), nil)
	out := ResolverStatus().Env([]string{"LD_PRELOAD=" + ShimPath})
	got, _ := preload(out)
	if got != ShimPath {
		t.Errorf("LD_PRELOAD = %q, want it left alone", got)
	}
}

func TestEnvFillsAnEmptyPreload(t *testing.T) {
	stub(t, platform.EmOS, present(), nil)
	got, _ := preload(ResolverStatus().Env([]string{"LD_PRELOAD="}))
	if got != ShimPath {
		t.Errorf("LD_PRELOAD = %q, want %q", got, ShimPath)
	}
}

// The controller reads `ok` and `size` through the same Kind machinery that
// reads spotify_status, so the vocabulary has to match. `needed` is the extra
// one: without it the controller cannot tell a FireOS device that does not
// want the file from an emOS device that is missing it.
func TestReportSpeaksTheControllersVocabulary(t *testing.T) {
	stub(t, platform.EmOS, present(), nil)
	rep := ResolverStatus().Report()
	if rep["ok"] != true || rep["needed"] != true {
		t.Errorf("installed on emOS: %v", rep)
	}
	if rep["size"] != int64(4824) {
		t.Errorf("size = %v, want 4824", rep["size"])
	}
	if _, ok := rep["reason"]; ok {
		t.Errorf("an installed file must carry no reason: %v", rep)
	}
	if rep["binary"] != ShimPath {
		t.Errorf("binary = %v, want %q", rep["binary"], ShimPath)
	}

	stub(t, platform.FireOS, nil, errors.New("gone"))
	rep = ResolverStatus().Report()
	if rep["ok"] != false || rep["needed"] != false {
		t.Errorf("absent on FireOS: %v", rep)
	}
	if rep["reason"] != "not_installed" {
		t.Errorf("reason = %v, want not_installed", rep["reason"])
	}
}

// The path both halves compare against. em_endpoint_bins has the mirror of
// this test; a disagreement installs the library where nothing looks for it
// and reports success.
func TestShimPathIsBesideTheEndpoints(t *testing.T) {
	if ShimPath != "/data/local/bin/gaishim.so" {
		t.Fatalf("ShimPath = %q; the controller's KINDS[\"gaishim\"].dest "+
			"must be changed with it", ShimPath)
	}
}
