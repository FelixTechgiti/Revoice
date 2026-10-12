package mcast

import (
	"encoding/hex"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// The two packets are pinned as bytes rather than as properties, because
// these exact ones are the measurement: sent by hand from Studio on
// 2026-10-12 they restored inbound multicast within nine seconds, and the
// controller's scan then read AirPlay as visible with its RTSP port
// reachable. A property test would pass on a packet nothing has ever
// answered.
const (
	wantV2 = "46c00020000000000102000000000000" + "e00000fb" + "94040000" +
		"1600" + "0904" + "e00000fb"
	wantV3 = "46c00028000000000102000000000000" + "e0000016" + "94040000" +
		"2200" + "fb02" + "00000001" + "02000000" + "e00000fb"
)

func TestTheReportsAreTheBytesTheDeviceAnswered(t *testing.T) {
	g := net.ParseIP(Group)
	if got := hex.EncodeToString(V2Report(g)); got != wantV2 {
		t.Errorf("v2 report\n got %s\nwant %s", got, wantV2)
	}
	if got := hex.EncodeToString(V3Report(g)); got != wantV3 {
		t.Errorf("v3 report\n got %s\nwant %s", got, wantV3)
	}
	// 32 and 40 are what the freestanding probe reported sending on the
	// device, so a change in length is a change the hardware has not seen.
	if n := len(V2Report(g)); n != 32 {
		t.Errorf("v2 report is %d bytes, the device answered 32", n)
	}
	if n := len(V3Report(g)); n != 40 {
		t.Errorf("v3 report is %d bytes, the device answered 40", n)
	}
}

// RFC 1071: a correct checksum makes the one-complement sum over the whole
// message come out all ones. Checked independently of the builder, so a
// checksum that is wrong in the same way twice still fails.
func TestEachReportCarriesACheckedSum(t *testing.T) {
	for _, tc := range []struct {
		name string
		pkt  []byte
	}{
		{"v2", V2Report(net.ParseIP(Group))},
		{"v3", V3Report(net.ParseIP(Group))},
	} {
		msg := tc.pkt[24:] // past the IP header and its Router Alert option
		var s uint32
		for i := 0; i+1 < len(msg); i += 2 {
			s += uint32(msg[i])<<8 | uint32(msg[i+1])
		}
		for s>>16 != 0 {
			s = s&0xFFFF + s>>16
		}
		if uint16(s) != 0xFFFF {
			t.Errorf("%s: the message does not sum to all ones, got %04x", tc.name, s)
		}
	}
}

// The Router Alert option is what a snooping switch looks for, and an IHL
// that does not account for it makes the kernel read the option as payload.
func TestTheHeaderAccountsForTheRouterAlertOption(t *testing.T) {
	p := V2Report(net.ParseIP(Group))
	if ihl := int(p[0]&0x0F) * 4; ihl != 24 {
		t.Errorf("IHL is %d bytes, the header plus Router Alert is 24", ihl)
	}
	if p[20] != 0x94 || p[21] != 0x04 {
		t.Errorf("Router Alert is %02x %02x, want 94 04", p[20], p[21])
	}
	if p[8] != 1 {
		t.Errorf("TTL is %d; IGMP never leaves the link", p[8])
	}
	if p[9] != 2 {
		t.Errorf("protocol is %d, IGMP is 2", p[9])
	}
	// The source address is left zero on purpose: the kernel fills it with
	// the route's own, which is what keeps this from disagreeing with the
	// address the responders advertise.
	for i := 12; i < 16; i++ {
		if p[i] != 0 {
			t.Fatalf("the source address is set; the kernel must fill it")
		}
	}
}

// A v3 report names its group INSIDE and is addressed to the all-routers
// group; a v2 report is addressed to the group itself. Swapping the two is a
// packet that is structurally valid and that no querier acts on.
func TestTheReportsGoToTheRightDestinations(t *testing.T) {
	v2 := V2Report(net.ParseIP(Group))
	if got := net.IP(v2[16:20]).String(); got != Group {
		t.Errorf("v2 is addressed to %s, want the group %s", got, Group)
	}
	v3 := V3Report(net.ParseIP(Group))
	if got := net.IP(v3[16:20]).String(); got != AllV3Routers {
		t.Errorf("v3 is addressed to %s, want %s", got, AllV3Routers)
	}
	if got := net.IP(v3[36:40]).String(); got != Group {
		t.Errorf("the v3 group record names %s, want %s", got, Group)
	}
	if v3[32] != 2 {
		t.Errorf("the v3 record type is %d, want MODE_IS_EXCLUDE (2)", v3[32])
	}
}

func TestAGroupThatIsNotAnAddressBuildsNothing(t *testing.T) {
	for _, bad := range []string{"", "not-an-address", "::1", "ff02::fb"} {
		if V2Report(net.ParseIP(bad)) != nil || V3Report(net.ParseIP(bad)) != nil {
			t.Errorf("%q built a packet; a typo must not reach the wire", bad)
		}
	}
}

// --- the Reporter's gates and cadence --------------------------------------

type sendLog struct {
	dsts []string
	err  error
}

func (s *sendLog) send(_ []byte, dst net.IP) error {
	s.dsts = append(s.dsts, dst.String())
	return s.err
}

func TestItReportsOnlyWhileSomethingHoldsTheGroup(t *testing.T) {
	for _, tc := range []struct {
		name           string
		active, joined bool
		want           bool
	}{
		{"both", true, true, true},
		{"nothing is running", false, true, false},
		{"the kernel has lost the group", true, false, false},
		{"neither", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s sendLog
			r := &Reporter{
				Send:   s.send,
				Active: func() bool { return tc.active },
				Joined: func() bool { return tc.joined },
			}
			if got := r.Tick(time.Now()); got != tc.want {
				t.Errorf("Tick = %v, want %v", got, tc.want)
			}
			if sent := len(s.dsts) > 0; sent != tc.want {
				t.Errorf("sent %d packets, want any = %v", len(s.dsts), tc.want)
			}
		})
	}
}

func TestItSendsBothReportsEachCadenceAndNotBetweenThem(t *testing.T) {
	var s sendLog
	r := &Reporter{
		Send:   s.send,
		Active: func() bool { return true },
		Joined: func() bool { return true },
		Every:  2 * time.Minute,
	}
	t0 := time.Date(2026, 10, 12, 16, 0, 0, 0, time.UTC)
	if !r.Tick(t0) {
		t.Fatal("the first tick did not report")
	}
	if len(s.dsts) != 2 || s.dsts[0] != Group || s.dsts[1] != AllV3Routers {
		t.Fatalf("one cadence sent %v, want the group then the all-routers group", s.dsts)
	}
	if r.Tick(t0.Add(119 * time.Second)) {
		t.Error("reported inside the cadence")
	}
	if !r.Tick(t0.Add(2 * time.Minute)) {
		t.Error("did not report at the cadence")
	}
	if r.Sent() != 2 {
		t.Errorf("Sent = %d, want 2", r.Sent())
	}
}

// The cadence has to stay inside what a snooping switch ages a membership out
// at, or the whole mechanism is a slower version of the fault.
func TestTheCadenceFitsTwiceInsideTheAgeingInterval(t *testing.T) {
	if DefaultReportEvery*2 > GroupMembershipInterval {
		t.Errorf("a lost report costs the membership: %s twice is past %s",
			DefaultReportEvery, GroupMembershipInterval)
	}
}

// A device that cannot send raw packets still has the endpoint-restart
// repair, so this must not become a line per cadence on the controller's log.
func TestAFailingSendIsCountedAndSaidOnce(t *testing.T) {
	s := sendLog{err: errors.New("operation not permitted")}
	r := &Reporter{
		Send:   s.send,
		Active: func() bool { return true },
		Joined: func() bool { return true },
		Every:  time.Minute,
	}
	t0 := time.Now()
	for i := 0; i < 5; i++ {
		if r.Tick(t0.Add(time.Duration(i) * time.Minute)) {
			t.Fatalf("tick %d reported success on a failing sender", i)
		}
	}
	if r.Failed() != 5 {
		t.Errorf("Failed = %d, want 5", r.Failed())
	}
	if r.Sent() != 0 {
		t.Errorf("Sent = %d, want 0", r.Sent())
	}
}

// The firmware has to actually run it. cmd/server.go is read as text because
// `cmd` cannot be imported here and is behind the `server` build tag, so a
// host `go test ./...` compiles none of it — the same reason the netfilter
// package checks its own wiring this way.
func TestTheFirmwareRunsTheReporter(t *testing.T) {
	b, err := os.ReadFile("../../cmd/server.go")
	if err != nil {
		t.Fatalf("cannot read cmd/server.go, so the wiring cannot be checked: %v", err)
	}
	src := string(b)
	for _, want := range []string{
		"mcast.RawReportSender",
		"mcast.Reporter{",
		// Both gates, or the device would assert a membership nothing holds.
		"Joined:",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("cmd/server.go does not mention %q: the reporter is built "+
				"and never runs, which is the fault it was written for", want)
		}
	}
}
