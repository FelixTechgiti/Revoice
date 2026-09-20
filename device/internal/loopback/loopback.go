// Bringing up the loopback interface, because nothing else on this device
// does.
//
// **There was no 127.0.0.1 on an emOS Echo at all** (#226). Linux does not
// configure `lo` by itself — Android's init does it, and emOS' init brings up
// `wlan0` and nothing else. The first program to care was AirPlay 2: nqptp
// binds its control port on the loopback and shairport-sync sends to it, so
// both died at startup, the second one because the first never came up:
//
//	nqptp is unable to listen on port 9000. The error is: 99,
//	"Cannot assign requested address".
//
// EADDRNOTAVAIL on a bind to 127.0.0.1 has one cause: the address is on no
// interface. nqptp's wildcard sockets on UDP 319 and 320 came up in the same
// breath, which is what makes the reading certain rather than likely.
//
// **Not gated on the base**, for the reason internal/netfilter is not: the
// question is about the INTERFACE, not about which userspace booted. A device
// whose loopback is already configured — every FireOS one, and any emOS one
// once its init does this too — takes the first branch and writes nothing. So
// this stays correct on a base nobody has thought about yet.
//
// It is also not the real home for it. emOS' init should configure the
// loopback so a device is right before our firmware starts and whether or not
// it ever does; this exists because the init ships in a boot image somebody
// has to flash and the firmware ships by OTA.
package loopback

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// Name of the interface. Not a parameter: there is one loopback, and a caller
// that could pass something else would be a caller that could point this at a
// real network interface and take it over.
const Name = "lo"

// Addr is what a loopback carries everywhere, and /8 with it. Written out
// rather than taken from net.IPv4loopback so the mask sits beside the address
// it belongs to.
var (
	Addr = net.IPv4(127, 0, 0, 1)
	Mask = net.IPv4(255, 0, 0, 0)
)

// State is what Ensure found and what it did about it.
type State struct {
	// AlreadyUp is true when nothing had to be written: the interface was up
	// and already carried the address. The ordinary case on FireOS.
	AlreadyUp bool
	// Configured is true when this call brought it up.
	Configured bool
}

// Ensure brings the loopback up with 127.0.0.1/8 if it is not already.
//
// Idempotent, and deliberately silent about the common case: it is called at
// every start, and a line per start about a thing that was already true is how
// a log stops being read.
func Ensure() (State, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return State{}, fmt.Errorf("loopback: socket: %w", err)
	}
	defer unix.Close(fd)

	if ok, err := configured(fd); err != nil {
		return State{}, err
	} else if ok {
		return State{AlreadyUp: true}, nil
	}

	// Address before flags. A kernel brought up with no address is an
	// interface that is UP and cannot be bound to, which is the state being
	// repaired here rather than a step towards it — and SIOCSIFADDR on a down
	// interface is ordinary.
	if err := setAddr(fd, unix.SIOCSIFADDR, Addr); err != nil {
		return State{}, err
	}
	if err := setAddr(fd, unix.SIOCSIFNETMASK, Mask); err != nil {
		return State{}, err
	}

	ifr, err := unix.NewIfreq(Name)
	if err != nil {
		return State{}, fmt.Errorf("loopback: ifreq: %w", err)
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr); err != nil {
		return State{}, fmt.Errorf("loopback: read flags: %w", err)
	}
	// OR rather than assign: the flags word carries what the kernel already
	// thinks about this interface, and clearing the rest of it to set one bit
	// is how an unrelated property gets turned off by a change that looked
	// like it only touched UP.
	ifr.SetUint16(ifr.Uint16() | unix.IFF_UP | unix.IFF_RUNNING)
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, ifr); err != nil {
		return State{}, fmt.Errorf("loopback: set flags: %w", err)
	}
	return State{Configured: true}, nil
}

// configured answers whether the interface is up AND carries the address.
//
// Both, not either. An interface that is up with no address and one that has
// an address and is down both fail to bind, and the second is the easier one
// to leave behind: SIOCGIFADDR answers from the kernel's record, which is set
// even while the interface is down.
func configured(fd int) (bool, error) {
	ifr, err := unix.NewIfreq(Name)
	if err != nil {
		return false, fmt.Errorf("loopback: ifreq: %w", err)
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr); err != nil {
		// No such interface is a real answer and not ours to repair: a kernel
		// with no loopback at all is a kernel, not a configuration.
		return false, fmt.Errorf("loopback: read flags: %w", err)
	}
	if ifr.Uint16()&unix.IFF_UP == 0 {
		return false, nil
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFADDR, ifr); err != nil {
		// EADDRNOTAVAIL here means exactly "no address yet", which is the
		// case this function exists to detect rather than an error to
		// report.
		return false, nil
	}
	have, err := ifr.Inet4Addr()
	if err != nil {
		return false, nil
	}
	return net.IP(have).Equal(Addr), nil
}

func setAddr(fd int, req uint, ip net.IP) error {
	ifr, err := unix.NewIfreq(Name)
	if err != nil {
		return fmt.Errorf("loopback: ifreq: %w", err)
	}
	if err := ifr.SetInet4Addr(ip.To4()); err != nil {
		return fmt.Errorf("loopback: %v: %w", ip, err)
	}
	if err := unix.IoctlIfreq(fd, req, ifr); err != nil {
		return fmt.Errorf("loopback: set %v: %w", ip, err)
	}
	return nil
}
