package mcast

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// RawReportSender opens the raw IGMP socket a Reporter sends through, and
// returns the send closure and a close.
//
// It needs root, which this firmware has, and it is the whole of the
// platform-specific half: everything that decides WHAT to send is in
// report.go and runs on the host.
//
// Three things are set and each is load-bearing:
//
//   - **SOCK_CLOEXEC.** This firmware has about twenty exec sites and two
//     long-lived children, and `internal/sndcloexec` exists because a
//     descriptor opened in C crossed every one of them. A raw socket
//     inherited by librespot is a capability handed to a third-party binary
//     for no reason; Go sets the flag when asked and never otherwise.
//   - **SO_BINDTODEVICE.** There is no 224.0.0.0/4 route on this device, so
//     a multicast send falls back to the default route's device. True today
//     and not something to rest a repair on.
//   - **IP_HDRINCL.** The IP header has to carry the Router Alert option,
//     which the kernel does not add, so the header is built here. The kernel
//     still fills in the identifier, the header checksum and a zero source
//     address — see ipHeader.
func RawReportSender(iface string) (func([]byte, net.IP) error, func() error, error) {
	if iface == "" {
		iface = Iface
	}
	fd, err := syscall.Socket(syscall.AF_INET,
		syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.IPPROTO_IGMP)
	if err != nil {
		return nil, nil, fmt.Errorf("raw IGMP socket: %w", err)
	}
	closeFd := func() error { return syscall.Close(fd) }
	if err := syscall.SetsockoptString(fd, syscall.SOL_SOCKET,
		syscall.SO_BINDTODEVICE, iface); err != nil {
		_ = closeFd()
		return nil, nil, fmt.Errorf("bind the IGMP socket to %s: %w", iface, err)
	}
	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP,
		syscall.IP_HDRINCL, 1); err != nil {
		_ = closeFd()
		return nil, nil, fmt.Errorf("IP_HDRINCL on the IGMP socket: %w", err)
	}
	send := func(pkt []byte, dst net.IP) error {
		v4 := dst.To4()
		if v4 == nil {
			return errors.New("the IGMP destination is not an IPv4 address")
		}
		var sa syscall.SockaddrInet4
		copy(sa.Addr[:], v4)
		return syscall.Sendto(fd, pkt, 0, &sa)
	}
	return send, closeFd, nil
}
