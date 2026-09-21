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

// ── the refusal, which is the whole point of the probe ──────────────────────

func stubProbe(t *testing.T, answer string) *int {
	t.Helper()
	calls := 0
	old := resolverProbe
	resolverProbe = func(string, string) (string, string) { calls++; return answer, "" }
	t.Cleanup(func() {
		resolverProbe = old
		logMu.Lock()
		lastRefusal = ""
		logMu.Unlock()
	})
	return &calls
}

// The state this exists for: the file is there, the linker will not take it,
// and every other signal reads healthy. Measured on 2026-09-21, when exactly
// that could not be told apart from two other explanations.
func TestARefusedPreloadIsReported(t *testing.T) {
	stub(t, platform.EmOS, present(), nil)
	stubProbe(t, `WARNING: linker: could not load library "gaishim.so"`)

	LogResolver(ResolverStatus(), "/data/local/bin/librespot")
	if PreloadRefusal() == "" {
		t.Fatal("the linker's complaint was not kept")
	}
	rep := ResolverStatus().Report()
	if rep["preload_error"] == nil {
		t.Error("Report must carry preload_error — `ok: true` alone says the " +
			"file is there, which is exactly the misleading half")
	}
	if rep["ok"] != true {
		t.Error("the file IS present; ok must stay true and the refusal ride beside it")
	}
}

func TestACleanLoadCarriesNoError(t *testing.T) {
	stub(t, platform.EmOS, present(), nil)
	stubProbe(t, "")

	LogResolver(ResolverStatus(), "/data/local/bin/librespot")
	if PreloadRefusal() != "" {
		t.Errorf("clean load reported a refusal: %q", PreloadRefusal())
	}
	if _, ok := ResolverStatus().Report()["preload_error"]; ok {
		t.Error("a clean load must add no preload_error key at all")
	}
}

// One spawn per state change, not one per endpoint restart. librespot restarts
// every 60s while it is failing, which is precisely when this would be asked.
func TestTheProbeRunsOncePerStateChange(t *testing.T) {
	stub(t, platform.EmOS, present(), nil)
	calls := stubProbe(t, "")

	for i := 0; i < 5; i++ {
		LogResolver(ResolverStatus(), "/data/local/bin/librespot")
	}
	if *calls != 1 {
		t.Errorf("probe ran %d times across five identical states, want 1", *calls)
	}
}

// A device that does not have the file must not be asked whether the linker
// likes it — there is nothing to load, and the answer would be noise.
func TestTheProbeIsNotRunWhenTheFileIsAbsent(t *testing.T) {
	stub(t, platform.EmOS, nil, errors.New("gone"))
	calls := stubProbe(t, "irrelevant")

	LogResolver(ResolverStatus(), "/data/local/bin/librespot")
	if *calls != 0 {
		t.Errorf("probe ran %d times with no file installed, want 0", *calls)
	}
}

// The lesson of 2026-09-21, in a test: the probe must be pointed at a binary,
// and the caller is the only one who knows which. With none, it answers
// nothing rather than guessing — the first version guessed `/system/bin/sh`,
// which is aarch64 on this board and rejected a perfectly good 32-bit
// library.
func TestNoBinaryMeansNoVerdict(t *testing.T) {
	le, st := PreloadProbe("/data/local/bin/gaishim.so", "")
	if le != "" || st != "" {
		t.Errorf("with no binary to probe both answers must be empty, got %q / %q", le, st)
	}
}

// A linker LINE is not a linker FAILURE, and reading one as the other
// reported a perfectly loaded library as REFUSED on 2026-09-21. Android 5.1
// prints "unused DT entry" for almost everything it loads, librespot
// included.
func TestOnlyARealFailureCountsAsARefusal(t *testing.T) {
	const noise = "WARNING: linker: /data/local/bin/librespot: unused DT entry: " +
		"type 0x6ffffef5 arg 0x1554\n" +
		"WARNING: linker: gaishim.so: unused DT entry: type 0x6fffffff arg 0x1\n"

	for _, tc := range []struct {
		name, stderr, want string
	}{
		{"nothing at all", "", ""},
		{"the program's own noise",
			"warning: config file missing\nusing defaults\n", ""},
		{"the linker's routine chatter — measured on a device", noise, ""},
		{"a hard refusal",
			`CANNOT LINK EXECUTABLE DEPENDENCIES: "/x/gaishim.so" is 32-bit instead of 64-bit`,
			`CANNOT LINK EXECUTABLE DEPENDENCIES: "/x/gaishim.so" is 32-bit instead of 64-bit`},
		{"a soft refusal buried in the chatter",
			noise + `WARNING: linker: could not load library "gaishim.so"` + "\n",
			`WARNING: linker: could not load library "gaishim.so"`},
		{"a missing symbol",
			"WARNING: linker: gaishim.so: cannot locate symbol \"getservbyname\"\n",
			`WARNING: linker: gaishim.so: cannot locate symbol "getservbyname"`},
	} {
		if got := linkerFailure(tc.stderr); got != tc.want {
			t.Errorf("%s: linkerFailure = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The shim's own answer, lifted out of whatever else the endpoint printed.
func TestTheSelfTestLineIsFoundAmongTheNoise(t *testing.T) {
	for _, tc := range []struct{ name, stderr, want string }{
		{"absent", "WARNING: linker: unused DT entry\n", ""},
		{"a success",
			"noise\ngaishim-selftest: clienttoken.spotify.com rc=0 ip=35.186.224.24 entries=1\nmore\n",
			"clienttoken.spotify.com rc=0 ip=35.186.224.24 entries=1"},
		{"a failure is still an answer",
			"gaishim-selftest: clienttoken.spotify.com rc=7 entries=0\n",
			"clienttoken.spotify.com rc=7 entries=0"},
	} {
		if got := selfTestLine(tc.stderr); got != tc.want {
			t.Errorf("%s: selfTestLine = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Loaded, and silent. That is its own state and must not read as success —
// it is what a library that is present but NOT the one being interposed
// looks like.
func TestALoadedButSilentShimIsItsOwnState(t *testing.T) {
	stub(t, platform.EmOS, present(), nil)
	old := resolverProbe
	resolverProbe = func(string, string) (string, string) { return "", "" }
	t.Cleanup(func() { resolverProbe = old })

	LogResolver(ResolverStatus(), "/data/local/bin/librespot")
	if PreloadRefusal() != "" || SelfTest() != "" {
		t.Error("a silent probe must record neither a refusal nor an answer")
	}
	rep := ResolverStatus().Report()
	if _, ok := rep["selftest"]; ok {
		t.Error("no self-test line means no selftest key")
	}
}

// What the device will actually report when it works.
func TestASuccessfulSelfTestReachesTheReport(t *testing.T) {
	stub(t, platform.EmOS, present(), nil)
	old := resolverProbe
	resolverProbe = func(string, string) (string, string) {
		return "", "clienttoken.spotify.com rc=0 ip=35.186.224.24 entries=1"
	}
	t.Cleanup(func() { resolverProbe = old })

	LogResolver(ResolverStatus(), "/data/local/bin/librespot")
	rep := ResolverStatus().Report()
	if rep["selftest"] == nil || rep["preload_error"] != nil {
		t.Errorf("a clean resolve must carry selftest and no preload_error: %v", rep)
	}
}

// A probe against a binary that cannot even be executed answers nothing about
// the linker, and must not answer "clean".
func TestATimedOutProbeIsNotSilence(t *testing.T) {
	old := probeTimeout
	probeTimeout = time.Nanosecond
	t.Cleanup(func() { probeTimeout = old })

	if le, _ := PreloadProbe("/x/gaishim.so", "/bin/sh"); le == "" {
		t.Error("a timed-out probe returned empty, which reads as a clean load")
	}
}
