// Package wifi implements safe WiFi network changes with automatic
// rollback, plus scan/status queries for the dashboard Connectivity tab.
//
// The mechanics mirror the provisioning wizard's runConfigWifi
// (controller/static/dashboard.jsx), which was hard-won on real hardware:
//
//   - The ONLY safe reload path is `svc wifi disable` + `svc wifi enable`.
//     The framework-managed wpa_supplicant instance auto-associates and
//     gets a DHCP lease on its own. Never use raw `start wpa_supplicant`,
//     kill -9, manual wpa_cli reconnect, or manual dhcpcd — a second bare
//     supplicant instance fights the framework one over wlan0 and the
//     interface dies (INTERFACE_DISABLED, never recovers).
//   - The config is a FULL replacement of wpa_supplicant.conf with a
//     single network block — no ambiguity about which AP it joins.
//   - wpa_cli needs BOTH -p /data/misc/wifi/sockets (non-default socket
//     dir) and -i wlan0.
//
// Two further lessons found on hardware 2026-07-11, AFTER the wizard:
//
//   - /system/bin/svc is a shebang-less shell script: execve (and hence
//     Go's exec.Command("svc", ...)) fails ENOEXEC. It must be run via
//     /system/bin/sh, and the disable must be VERIFIED to have dropped
//     association — a silent no-op bounce makes every gate below pass
//     against the old network and falsely commits the new conf.
//   - The conf must be written while WiFi is DOWN: on `svc wifi disable`,
//     WifiStateMachine saves its in-memory network list back over
//     wpa_supplicant.conf, clobbering anything written beforehand (the
//     wizard got away with write-then-bounce only because a factory
//     device has no framework-known networks to save). See reloadConf.
//
// Unlike the wizard (ADB shell), this package runs inside the root Go
// binary, so file writes use plain os.WriteFile — none of the mksh
// redirect quirks apply. Ownership must still be restored to wifi:wifi
// (AID_WIFI=1010) mode 0660 or the framework can't read the config.
//
// Safety model (the connection to the controller dies mid-change, so the
// device owns the whole sequence):
//
//  1. Back up the current conf and drop a pending marker file.
//  2. Disable wifi (verified), write the new conf, enable wifi.
//  3. Gates: associate to the TARGET SSID ≤45s → IPv4 on wlan0 ≤20s →
//     control WebSocket re-registered ≤90s. Any failure → restore the
//     backup the same way and report the failure once the connection
//     returns.
//  4. On success the controller sends wifi_commit, which deletes the
//     marker + backup. Until then the change is provisional.
//  5. Crash safety: if the marker exists at process start, a previous
//     switch never got committed — RecoverIfPending restores the backup
//     and bounces, so a crash or power cycle mid-switch self-heals back
//     to the old network (same philosophy as the A/B binary slots).
package wifi

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	confPath   = "/data/misc/wifi/wpa_supplicant.conf"
	backupPath = "/data/misc/wifi/wpa_supplicant.conf.revoice-bak"
	markerPath = "/data/local/tmp/revoice_wifi_pending"

	wpaSockDir = "/data/misc/wifi/sockets"
	iface      = "wlan0"

	// AID_WIFI — fixed uid/gid on Android; the framework reads the conf
	// as this user.
	aidWifi = 1010

	// 20s (the provisioning wizard's window) proved too tight on hardware
	// for a network the framework hasn't joined before — autojoin's scan
	// cycle alone can eat most of it. Reverts re-associate to a known
	// network well inside 20s, so only first-join pays the longer wait.
	associateTimeout = 45 * time.Second
	ipTimeout        = 20 * time.Second
	// The reconnect gate covers mDNS rediscovery plus the control client's
	// 5s retry cadence; generous because a false negative reverts a
	// perfectly good network change.
	reconnectTimeout = 90 * time.Second
)

// Result is the outcome of a change attempt, reported to the controller
// as a wifi_result message once a connection exists to carry it.
type Result struct {
	OK    bool   `json:"ok"`
	SSID  string `json:"ssid"`
	Error string `json:"error,omitempty"`
}

// Network is one scan result row. SSID is for display; SSIDHex is the exact
// bytes, which is what a change request sends back (see ssid.go).
type Network struct {
	SSID    string `json:"ssid"`
	SSIDHex string `json:"ssid_hex"`
	Signal  int    `json:"signal"`
}

type marker struct {
	NewSSID   string `json:"newSsid"`
	StartedAt int64  `json:"startedAt"`
}

var (
	mu       sync.Mutex
	inFlight bool
	// pending holds an unreported Result until the controller connection
	// can carry it (drained by TakeResult from the OnConnected callback).
	pending *Result
)

// ─── Queries ──────────────────────────────────────────────────────────────────

func wpaCli(args ...string) (string, error) {
	full := append([]string{"-p", wpaSockDir, "-i", iface}, args...)
	out, err := exec.Command("wpa_cli", full...).CombinedOutput()
	return string(out), err
}

// CurrentSSID returns the associated SSID for display, or "" when not
// associated. Never use it to address a network — that is what the bytes are
// for (ssid.go).
func CurrentSSID() string {
	b := currentSSIDBytes()
	if b == nil {
		return ""
	}
	return SSIDText(b)
}

// currentSSIDBytes is the associated SSID's exact bytes, or nil. The status
// line is printf_encode'd like scan_results, and only the trailing CR a line
// can carry is stripped: spaces at either end are part of an SSID.
func currentSSIDBytes() []byte {
	out, _ := wpaCli("status")
	if !strings.Contains(out, "wpa_state=COMPLETED") {
		return nil
	}
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "ssid="); ok {
			return UnescapeSSID(v)
		}
	}
	return nil
}

// currentIPv4 returns the interface's IPv4 address, or "".
func currentIPv4() string {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return ""
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if v4 := ipn.IP.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}

// Scan triggers a wpa_cli scan and returns networks sorted strongest
// first, deduped by SSID (strongest AP wins — multiple APs/bands share
// SSIDs). Safe while associated; expect a brief audio-free RF glitch.
func Scan() ([]Network, error) {
	if _, err := wpaCli("scan"); err != nil {
		return nil, fmt.Errorf("scan trigger: %w", err)
	}
	time.Sleep(4 * time.Second)
	out, err := wpaCli("scan_results")
	if err != nil {
		return nil, fmt.Errorf("scan_results: %w", err)
	}

	return parseScan(out), nil
}

// parseScan turns scan_results into one Network per SSID, strongest AP first.
// Keyed by the SSID's BYTES, so two names that only differ in bytes a display
// cannot show stay two networks.
func parseScan(out string) []Network {
	best := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		// bssid \t frequency \t signal \t flags \t ssid. Tabs inside an SSID
		// arrive escaped, so the fifth field is the whole name; only a
		// transport CR is stripped, never spaces.
		parts := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(parts) < 5 {
			continue
		}
		ssid := UnescapeSSID(parts[4])
		// Hidden networks advertise an empty or all-zero SSID: unjoinable by
		// name, so dropped.
		if hiddenOrEmpty(ssid) {
			continue
		}
		sig, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil {
			continue
		}
		key := string(ssid)
		if cur, ok := best[key]; !ok || sig > cur {
			best[key] = sig
		}
	}
	nets := make([]Network, 0, len(best))
	for key, sig := range best {
		b := []byte(key)
		nets = append(nets, Network{SSID: SSIDText(b), SSIDHex: hex.EncodeToString(b), Signal: sig})
	}
	sort.Slice(nets, func(i, j int) bool { return nets[i].Signal > nets[j].Signal })
	return nets
}

// ─── Change with rollback ─────────────────────────────────────────────────────

func getprop(key, fallback string) string {
	out, err := exec.Command("getprop", key).Output()
	if err != nil {
		return fallback
	}
	if v := strings.TrimSpace(string(out)); v != "" {
		return v
	}
	return fallback
}

// composeConf builds the full-replacement wpa_supplicant.conf — the same
// template the provisioning wizard writes. An empty psk produces an open
// (key_mgmt=NONE) network block.
//
// The SSID and the passphrase go through ssidLine/pskLine rather than %q: an
// SSID is 0–32 arbitrary octets and Go's %q escapes what wpa_supplicant reads
// literally, so `Café` came back out as `Caf\xc3\xa9` and a name holding a
// quote could not be written at all.
func composeConf(ssid []byte, psk string) string {
	network := []string{
		"network={",
		ssidLine(ssid),
	}
	if psk == "" {
		network = append(network, "\tkey_mgmt=NONE")
	} else {
		network = append(network,
			pskLine(psk),
			"\tkey_mgmt=WPA-PSK",
		)
	}
	network = append(network, "\tpriority=1", "}")

	lines := []string{
		"ctrl_interface=" + wpaSockDir,
		"driver_param=use_p2p_group_interface=1",
		"update_config=1",
		"device_name=" + getprop("ro.product.name", "revoice"),
		"manufacturer=" + getprop("ro.product.manufacturer", "Amazon"),
		"model_name=" + getprop("ro.product.model", "AEOBC"),
		"model_number=" + getprop("ro.product.model", "AEOBC"),
		"serial_number=" + getprop("ro.serialno", getprop("ro.boot.serialno", "unknown")),
		"device_type=1-0050F204-9",
		"os_version=01020300",
		"config_methods=physical_display virtual_push_button",
		"p2p_no_group_iface=1",
		"external_sim=1",
		"wowlan_triggers=disconnect",
	}
	lines = append(lines, network...)
	return strings.Join(lines, "\n") + "\n"
}

func writeConf(content string) error {
	// Traverse bit on the dir — 666 here made every file inside
	// unopenable (provisioning finding).
	_ = os.Chmod("/data/misc/wifi", 0o770)
	if err := os.WriteFile(confPath, []byte(content), 0o660); err != nil {
		return fmt.Errorf("write %s: %w", confPath, err)
	}
	if err := os.Chown(confPath, aidWifi, aidWifi); err != nil {
		return fmt.Errorf("chown %s: %w", confPath, err)
	}
	return os.Chmod(confPath, 0o660)
}

// svcWifi toggles the framework WiFi service. /system/bin/svc is a
// shebang-less shell script — execve returns ENOEXEC on it, so it must be
// run through sh explicitly (exec.Command("svc", ...) silently no-ops).
func svcWifi(state string) error {
	out, err := exec.Command("/system/bin/sh", "/system/bin/svc", "wifi", state).CombinedOutput()
	if err != nil {
		return fmt.Errorf("svc wifi %s: %v (%s)", state, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// disableWifi brings the framework WiFi down and verifies it actually
// dropped. A no-op disable leaves wpa_supplicant running against its old
// in-memory config, and every downstream gate then passes vacuously
// against the old network — a false success that commits an untried conf.
func disableWifi() error {
	log.Println("[wifi] svc wifi disable")
	if err := svcWifi("disable"); err != nil {
		return err
	}
	if !waitFor("disassociation after disable", 10*time.Second, func() bool { return !associated() }) {
		_ = svcWifi("enable")
		return fmt.Errorf("wifi did not go down after 'svc wifi disable'")
	}
	return nil
}

func enableWifi() error {
	log.Println("[wifi] svc wifi enable")
	if err := svcWifi("enable"); err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	return nil
}

// reloadConf swaps in a new wpa_supplicant.conf with WiFi DOWN. Order is
// load-bearing: on disable, WifiStateMachine saves its in-memory network
// list back to wpa_supplicant.conf — a conf written while WiFi is up gets
// clobbered by that save, and the device silently rejoins the old network
// (found on hardware 2026-07-11; provisioning never hit it because a
// factory device has no framework-known networks to save).
// The per-base half lives in reload.go: emOS has no framework and no `svc`,
// so the Android lever below is not merely wrong there, it is absent.
func reloadConf(content string) error {
	return reloadWith(pickSupplicant(), content)
}

func waitFor(what string, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Second)
	}
	log.Printf("[wifi] timed out waiting for %s (%s)", what, timeout)
	return false
}

func associated() bool {
	out, _ := wpaCli("status")
	return strings.Contains(out, "wpa_state=COMPLETED")
}

// associatedTo reports association specifically to the named network —
// bare wpa_state=COMPLETED is satisfied by the *old* network if the
// supplicant never actually restarted.
// associatedTo compares the SSID's BYTES, never its display form: two names
// that render the same can be different networks, and SSIDText replaces
// anything that is not valid UTF-8 with one replacement character — so a
// comparison on the text answers yes for two networks that merely both hold
// an unprintable byte.
func associatedTo(ssid []byte) bool {
	return bytes.Equal(currentSSIDBytes(), ssid)
}

// waitForAssociation polls for association to ssid, logging the raw
// supplicant state every 5s so a timeout in the field says what the
// framework was doing (SCANNING vs 4WAY_HANDSHAKE vs INTERFACE_DISABLED).
func waitForAssociation(ssid []byte, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	lastDiag := time.Now()
	for time.Now().Before(deadline) {
		if associatedTo(ssid) {
			return true
		}
		if time.Since(lastDiag) >= 5*time.Second {
			out, _ := wpaCli("status")
			state := "?"
			for _, line := range strings.Split(out, "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "wpa_state="); ok {
					state = v
					break
				}
			}
			log.Printf("[wifi] waiting for association to %q — wpa_state=%s", SSIDText(ssid), state)
			lastDiag = time.Now()
		}
		time.Sleep(time.Second)
	}
	log.Printf("[wifi] timed out waiting for association to %q (%s)", SSIDText(ssid), timeout)
	return false
}

func setResult(r Result) {
	mu.Lock()
	pending = &r
	mu.Unlock()
}

// PendingResult returns the unacknowledged change outcome, if any,
// WITHOUT clearing it. Delivery is at-least-once: the result stays
// pending (and is re-sent on reconnect and on a retry ticker) until the
// controller acks with wifi_commit — a fire-and-forget send can vanish
// into a half-open TCP connection that the interface bounce killed but
// that still looks connected to the writer (seen on hardware 2026-07-11).
func PendingResult() *Result {
	mu.Lock()
	defer mu.Unlock()
	if pending == nil {
		return nil
	}
	r := *pending
	return &r
}

// Commit handles the controller's wifi_commit ack: the provisional state
// (marker + backup) is deleted so a future crash/restart keeps the new
// network, and the pending result stops being re-sent. The controller
// acks failure results too — the revert already removed marker/backup,
// so the removes are harmless no-ops there.
func Commit() {
	_ = os.Remove(markerPath)
	_ = os.Remove(backupPath)
	mu.Lock()
	pending = nil
	mu.Unlock()
	log.Println("[wifi] result acknowledged — backup and pending marker removed")
}

// Change switches to a new network with automatic rollback. Runs
// synchronously (call from a goroutine); connected must report whether
// the control WebSocket is currently registered with the controller.
// The outcome lands in TakeResult either way.
func Change(ssidBytes []byte, psk string, connected func() bool) {
	// Everything user-facing — the result, the marker, the log lines — carries
	// the SSID's DISPLAY form; everything that addresses the network carries
	// its bytes. See ssid.go.
	ssid := SSIDText(ssidBytes)
	mu.Lock()
	if inFlight {
		mu.Unlock()
		setResult(Result{OK: false, SSID: ssid, Error: "another WiFi change is already in progress"})
		return
	}
	inFlight = true
	pending = nil
	mu.Unlock()
	defer func() {
		mu.Lock()
		inFlight = false
		mu.Unlock()
	}()

	if err := validate(ssidBytes, psk); err != nil {
		setResult(Result{OK: false, SSID: ssid, Error: err.Error()})
		return
	}

	log.Printf("[wifi] change requested → %q", ssid)

	old, err := os.ReadFile(confPath)
	if err != nil {
		setResult(Result{OK: false, SSID: ssid, Error: fmt.Sprintf("cannot read current config: %v", err)})
		return
	}
	if err := os.WriteFile(backupPath, old, 0o600); err != nil {
		setResult(Result{OK: false, SSID: ssid, Error: fmt.Sprintf("cannot write backup: %v", err)})
		return
	}
	mk, _ := json.Marshal(marker{NewSSID: ssid, StartedAt: time.Now().Unix()})
	if err := os.WriteFile(markerPath, mk, 0o600); err != nil {
		setResult(Result{OK: false, SSID: ssid, Error: fmt.Sprintf("cannot write pending marker: %v", err)})
		return
	}

	revert := func(reason string) {
		log.Printf("[wifi] change to %q failed (%s) — reverting", ssid, reason)
		// Restore with WiFi down (see reloadConf) — but if the restore
		// write fails, conf is beyond self-healing: leave the marker so
		// RecoverIfPending retries on next start.
		restoreErr := reloadConf(string(old))
		if restoreErr != nil {
			log.Printf("[wifi] REVERT FAILED: %v — marker left for recovery on restart", restoreErr)
		} else {
			_ = os.Remove(markerPath)
			_ = os.Remove(backupPath)
		}
		waitFor("re-association after revert", associateTimeout, associated)
		setResult(Result{OK: false, SSID: ssid, Error: reason})
	}

	if err := reloadConf(composeConf(ssidBytes, psk)); err != nil {
		revert(err.Error())
		return
	}

	if !waitForAssociation(ssidBytes, associateTimeout) {
		revert(fmt.Sprintf("did not associate to %q within %s (wrong passphrase or AP out of range?)", ssid, associateTimeout))
		return
	}
	log.Printf("[wifi] associated to %q", ssid)

	if !waitFor("IPv4 address", ipTimeout, func() bool { return currentIPv4() != "" }) {
		revert(fmt.Sprintf("associated to %q but no IP within %s (DHCP problem?)", ssid, ipTimeout))
		return
	}
	log.Printf("[wifi] got IP %s", currentIPv4())

	if !waitFor("controller reconnect", reconnectTimeout, connected) {
		revert(fmt.Sprintf("joined %q (IP %s) but could not reach the controller within %s — wrong VLAN or isolated network?", ssid, currentIPv4(), reconnectTimeout))
		return
	}

	// Connected on the new network. Marker + backup stay until the
	// controller acknowledges with wifi_commit.
	log.Printf("[wifi] change to %q succeeded — awaiting commit from controller", ssid)
	setResult(Result{OK: true, SSID: ssid})
}

// RecoverIfPending restores the pre-change config if a previous change
// never got committed (crash, power cycle, or a failed revert). Call
// once at process start, before the control client runs.
func RecoverIfPending() {
	mk, err := os.ReadFile(markerPath)
	if err != nil {
		return // no pending change — the normal case
	}
	var m marker
	_ = json.Unmarshal(mk, &m)
	log.Printf("[wifi] uncommitted change to %q found at startup — restoring previous network", m.NewSSID)

	backup, err := os.ReadFile(backupPath)
	if err != nil {
		// Marker without backup: the change already reverted its conf but
		// couldn't remove the marker, or the backup was lost. Nothing to
		// restore from — clear the marker and carry on with whatever conf
		// is in place.
		log.Printf("[wifi] no backup to restore (%v) — clearing marker", err)
		_ = os.Remove(markerPath)
		return
	}
	if err := reloadConf(string(backup)); err != nil {
		log.Printf("[wifi] startup restore failed: %v — leaving marker for next start", err)
		return
	}
	_ = os.Remove(markerPath)
	_ = os.Remove(backupPath)
	setResult(Result{OK: false, SSID: m.NewSSID, Error: "device restarted before the change was confirmed — previous network restored"})
}
