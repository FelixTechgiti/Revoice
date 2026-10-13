package mdnsannounce

import (
	"fmt"
	"log"
	"net"
	"sync/atomic"
)

// Group and Port are the mDNS link-local group, RFC 6762 §3.
const (
	Group = "224.0.0.251"
	Port  = 5353
)

// MaxPacket bounds a read. mDNS allows a message up to the path MTU and in
// practice an announcement from either responder is a few hundred bytes; this
// is generous and still a fixed buffer.
const MaxPacket = 9000

// Listener is the one socket this package uses, for both halves of the job.
//
// It is bound to the GROUP address rather than the wildcard, so it receives
// only what was addressed to 224.0.0.251 and never a unicast reply meant for
// somebody's query. Binding a multicast address makes the kernel pick the
// source address from the outgoing device when we send (Linux zeroes the
// socket's source address for a multicast or broadcast bind), so an
// announcement leaves carrying the interface's own address, which is what the
// A record inside it says.
//
// Sending from the same socket is deliberate and is the spec's requirement
// rather than a saving: RFC 6762 §6 says every Multicast DNS response MUST
// have source port 5353, and a conforming receiver discards one that does
// not. An ephemeral source port is legal for a one-shot query — which is what
// `device/tools/mdnsprobe` does — and wrong for what this sends.
type Listener struct {
	conn *net.UDPConn
	addr *net.UDPAddr
	ifi  *net.Interface

	// foreign counts packets that reached the group from SOMEBODY ELSE. See
	// ForeignCount for why the count lives on the socket rather than on the
	// firewall, where it was measuring this device's own announcements.
	foreign atomic.Int64
}

// Open binds the mDNS group on iface and joins it.
//
// It coexists with the responders, which hold the same port: Go sets
// SO_REUSEADDR on a multicast listener, and so does every mDNS stack, because
// sharing 5353 with whatever else on the host speaks mDNS is the normal case
// rather than an edge.
func Open(iface string) (*Listener, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("interface %s: %w", iface, err)
	}
	gaddr := &net.UDPAddr{IP: net.ParseIP(Group), Port: Port}
	conn, err := net.ListenMulticastUDP("udp4", ifi, gaddr)
	if err != nil {
		return nil, fmt.Errorf("listen %s on %s: %w", gaddr, iface, err)
	}
	// ListenMulticastUDP also sets IP_MULTICAST_IF to ifi and turns
	// IP_MULTICAST_LOOP off. Both are wanted. The loopback flag is a
	// property of the SENDING socket on Linux, so turning it off here stops
	// our replay from reaching the two responders on this host — which would
	// otherwise look to them like somebody else claiming their names — and
	// does nothing to the loopback copies of THEIR announcements, which is
	// the whole input to this package.
	return &Listener{conn: conn, addr: gaddr, ifi: ifi}, nil
}

// Close releases the socket.
func (l *Listener) Close() error {
	if l == nil || l.conn == nil {
		return nil
	}
	return l.conn.Close()
}

// Send puts one packet on the group.
func (l *Listener) Send(pkt []byte) error {
	if l == nil || l.conn == nil {
		return fmt.Errorf("mdns listener is not open")
	}
	_, err := l.conn.WriteToUDP(pkt, l.addr)
	return err
}

// Addr returns the interface's current IPv4 address as text, or "" when it
// has none. Used only to notice a change.
func (l *Listener) Addr() (string, error) {
	if l == nil || l.ifi == nil {
		return "", fmt.Errorf("mdns listener is not open")
	}
	// Re-read the interface rather than reusing the one from Open: Addrs()
	// on a stale net.Interface still queries the kernel by index, but the
	// name-to-index mapping is what a re-created interface changes.
	ifi, err := net.InterfaceByName(l.ifi.Name)
	if err != nil {
		return "", err
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return "", err
	}
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if v4 := n.IP.To4(); v4 != nil {
			return v4.String(), nil
		}
	}
	return "", nil
}

// Watch reads the group until the socket is closed, offering to cache every
// packet this host sent itself.
//
// The source check is what makes this OUR announcements rather than the
// network's. A packet from any other host on the LAN is somebody else's
// records, and repeating those would make this device answer for a name it
// does not own — the one way a well-meant cadence turns into a fault on
// somebody else's equipment.
func (l *Listener) Watch(c *Cache, local func() (string, error)) {
	if l == nil || l.conn == nil || c == nil {
		return
	}
	buf := make([]byte, MaxPacket)
	for {
		n, src, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			// The socket is closed on shutdown, and there is no other
			// recoverable error on a bound multicast socket worth a retry
			// loop here: the firmware restarts the whole package if it has to.
			return
		}
		if src == nil || n <= 0 {
			continue
		}
		l.note(c, buf[:n], src.IP, local)
	}
}

// note decides what one arrival is: this device's own announcement, worth
// keeping so it can be repeated, or somebody else's, worth COUNTING so the
// deafness probe has an instrument that is about the network (#384).
//
// Split out of the read loop so both halves can be driven without a socket —
// a host test cannot join a multicast group, and the decision is the part
// worth pinning.
func (l *Listener) note(c *Cache, pkt []byte, src net.IP, local func() (string, error)) {
	mine, err := local()
	if err != nil || mine == "" {
		// Failure to look is not evidence, and here that cuts both ways: a
		// packet whose origin cannot be decided is neither ours to cache nor
		// somebody else's to count.
		return
	}
	if src == nil {
		return
	}
	if src.String() != mine {
		l.foreign.Add(1)
		return
	}
	c.Observe(pkt)
}

// Listen opens the socket and starts watching the group. It is called before
// the endpoints are started, so the announcements they make on the way up are
// the ones that get cached; an error means the device goes on behaving
// exactly as it did before this package existed.
func Listen(iface string, c *Cache) (*Listener, error) {
	l, err := Open(iface)
	if err != nil {
		return nil, err
	}
	go l.Watch(c, l.Addr)
	log.Printf("[mdnsannounce] listening on %s:%d via %s, to repeat this "+
		"device's own announcements every %s", Group, Port, iface, DefaultInterval)
	return l, nil
}

// Announcer returns the ticker that repeats what has been heard. It is built
// after the endpoints exist, because whether anything is published and what
// it is called are questions only they can answer.
func (l *Listener) Announcer(c *Cache, identity func() (string, error), active func() bool) *Announcer {
	return &Announcer{
		Cache: c,
		Send:  l.Send,
		Identity: func() (string, error) {
			addr, err := l.Addr()
			if err != nil {
				return "", err
			}
			name, err := identity()
			if err != nil {
				return "", err
			}
			return addr + "\x00" + name, nil
		},
		Active: active,
	}
}
