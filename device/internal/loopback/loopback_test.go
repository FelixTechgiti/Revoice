package loopback

import (
	"net"
	"testing"

	"golang.org/x/sys/unix"
)

// What a host CAN answer, and what it cannot.
//
// The repair itself needs root and a device whose loopback is down, which is
// neither a CI runner nor any machine anybody writes this on. What a runner
// DOES have is a correctly configured loopback — so it can answer the question
// that decides whether this code ever writes anything, and that is the half
// with a cost when it is wrong: a detector that reads a healthy interface as
// broken would reconfigure `lo` on every device at every start.

func TestAHealthyLoopbackIsLeftAlone(t *testing.T) {
	st, err := Ensure()
	if err != nil {
		t.Fatalf("Ensure on a machine with a working loopback: %v", err)
	}
	if !st.AlreadyUp {
		t.Fatalf("a configured loopback was reported as needing work: %+v", st)
	}
	if st.Configured {
		t.Fatal("nothing should have been written")
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	// The call site runs at every start, so this is the path that actually
	// executes in the field on every device that is already correct.
	for i := 0; i < 3; i++ {
		if _, err := Ensure(); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
}

func TestTheDetectorWantsBothUpAndTheAddress(t *testing.T) {
	// Stated as a property of the source rather than driven, because neither
	// half can be taken away on a host: an interface that is up with no
	// address and one that has an address and is down BOTH fail to bind, and
	// SIOCGIFADDR answers from the kernel's record even while the interface
	// is down — so checking only the address would call a down loopback fine.
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Skipf("no AF_INET socket here: %v", err)
	}
	defer unix.Close(fd)

	ok, err := configured(fd)
	if err != nil {
		t.Fatalf("configured: %v", err)
	}
	if !ok {
		t.Fatal("this machine's loopback reads as unconfigured")
	}
}

func TestAMissingInterfaceIsAnError(t *testing.T) {
	// A kernel with no loopback is a kernel, not a configuration, and must
	// not be silently "repaired" — the ioctl would fail anyway, and reporting
	// it is what puts the reason in the log.
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Skipf("no AF_INET socket here: %v", err)
	}
	defer unix.Close(fd)

	ifr, err := unix.NewIfreq("em-no-such-if")
	if err != nil {
		t.Fatalf("ifreq: %v", err)
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr); err == nil {
		t.Fatal("an interface that does not exist answered SIOCGIFFLAGS")
	}
}

func TestTheAddressAndMaskAreTheLoopbackOnes(t *testing.T) {
	// Cheap, and it is the one thing a typo here would get wrong silently:
	// a wrong address configures the interface successfully and leaves every
	// bind to 127.0.0.1 failing exactly as before.
	if !Addr.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("address is %v", Addr)
	}
	if !Mask.Equal(net.IPv4(255, 0, 0, 0)) {
		t.Fatalf("mask is %v", Mask)
	}
	if Name != "lo" {
		t.Fatalf("interface is %q", Name)
	}
}
