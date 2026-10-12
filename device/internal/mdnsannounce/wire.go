// Package mdnsannounce makes this device announce itself on a cadence instead
// of waiting to be asked.
//
// # Why
//
// Inbound multicast on this board stops arriving for minutes at a time while
// everything else on the link stays perfect (internal/mcast/allmulti.go has
// the measurements). A responder that cannot hear a query cannot answer one,
// so the Echo drops out of the AirPlay and Spotify Connect pickers — the
// symptom the owner reports as "it was there a moment ago". Two repairs have
// been tried against the reception side and neither holds.
//
// Transmission is unaffected throughout. Measured on Studio 2026-10-12, a
// device whose `Udp: InDatagrams` had been frozen for eight minutes still
// reached four other hosts with `ping 224.0.0.251`. So the device can still
// make itself known; it just cannot be asked. mDNS allows exactly that:
// RFC 6762 §8.3 unsolicited announcements are a normal response packet sent
// to the group with no question behind it, and every browser treats one as a
// cache update. Re-sending inside the records' own TTL keeps the entry alive
// in every picker on the LAN without the device ever hearing anything.
//
// # What is re-sent, and why it is not composed here
//
// The firmware does not model shairport-sync's or librespot's records. It
// listens on the mDNS group for the announcements its OWN responders make —
// which arrive through the kernel's multicast loopback and therefore do not
// depend on the radio at all — keeps the packets verbatim, and sends those
// bytes again. A record this file never parsed cannot be a record this file
// got wrong, and `pk=`, `ft=` and the rest go on being whatever the receiver
// that owns them says they are.
//
// # The one thing that MUST be parsed
//
// A goodbye packet is an ordinary response whose records carry TTL 0
// (RFC 6762 §10.1), and both responders send one as they shut down. Replaying
// a goodbye every minute would remove this device from every picker on the
// network and hold it there — worse than the fault. So each packet is walked
// far enough to see every record's TTL, and one with a zero is refused. The
// walk is a skip over names and record data (RFC 1035 §4.1), never an
// interpretation of them.
package mdnsannounce

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// HeaderLen is the fixed DNS message header, RFC 1035 §4.1.1.
const HeaderLen = 12

// ErrShort is returned for a message that ends inside a field. It is its own
// error so a caller can tell a truncated read from a well-formed packet that
// simply is not an announcement.
var ErrShort = errors.New("message ends mid-record")

// Message is what Inspect could establish about a packet without interpreting
// any record.
type Message struct {
	// Response is the QR bit, RFC 1035 §4.1.1. An mDNS announcement is a
	// response; a query is not ours to repeat.
	Response bool
	// Records is how many resource records the message carries across the
	// answer, authority and additional sections. It is the richness measure
	// the cache ranks by: an announcement carrying PTR, SRV, TXT and A keeps
	// a browser's entry whole, where a bare PTR answer to somebody's query
	// keeps only its name.
	Records int
	// Answers is ANCOUNT alone. A response with no answer at all says
	// nothing and is not worth repeating.
	Answers int
	// Goodbye reports that some record carries TTL 0 — RFC 6762 §10.1, the
	// way a responder withdraws a name. Never repeat one.
	Goodbye bool
}

// Inspect walks pkt far enough to fill a Message, and returns an error for
// anything it cannot walk.
//
// Everything here is a skip. Names are stepped over by length (RFC 1035
// §4.1.4: a length byte below 64, or a two-byte pointer that ends the name),
// record data by its own RDLENGTH, and nothing is decoded. A compression
// pointer is never FOLLOWED, so no cycle is possible and a packet cannot cost
// more than its own length to inspect.
func Inspect(pkt []byte) (Message, error) {
	if len(pkt) < HeaderLen {
		return Message{}, fmt.Errorf("%w: %d bytes, header is %d",
			ErrShort, len(pkt), HeaderLen)
	}
	var m Message
	flags := binary.BigEndian.Uint16(pkt[2:4])
	m.Response = flags&0x8000 != 0
	qd := int(binary.BigEndian.Uint16(pkt[4:6]))
	an := int(binary.BigEndian.Uint16(pkt[6:8]))
	ns := int(binary.BigEndian.Uint16(pkt[8:10]))
	ar := int(binary.BigEndian.Uint16(pkt[10:12]))
	m.Answers = an

	off := HeaderLen
	for i := 0; i < qd; i++ {
		var err error
		if off, err = skipName(pkt, off); err != nil {
			return Message{}, fmt.Errorf("question %d: %w", i, err)
		}
		// QTYPE and QCLASS.
		if off+4 > len(pkt) {
			return Message{}, fmt.Errorf("question %d: %w", i, ErrShort)
		}
		off += 4
	}
	for i := 0; i < an+ns+ar; i++ {
		var err error
		if off, err = skipName(pkt, off); err != nil {
			return Message{}, fmt.Errorf("record %d: %w", i, err)
		}
		// TYPE, CLASS, TTL, RDLENGTH — RFC 1035 §4.1.3.
		if off+10 > len(pkt) {
			return Message{}, fmt.Errorf("record %d: %w", i, ErrShort)
		}
		ttl := binary.BigEndian.Uint32(pkt[off+4 : off+8])
		rdlen := int(binary.BigEndian.Uint16(pkt[off+8 : off+10]))
		off += 10
		if off+rdlen > len(pkt) {
			return Message{}, fmt.Errorf("record %d rdata: %w", i, ErrShort)
		}
		off += rdlen
		if ttl == 0 {
			m.Goodbye = true
		}
		m.Records++
	}
	return m, nil
}

// skipName steps over a domain name and returns the offset after it.
//
// The top two bits of a length byte say what it is (RFC 1035 §4.1.4): 00 is a
// label of that length, 11 is a 14-bit pointer which ends the name, and 01
// and 10 are reserved and have never been assigned. A reserved form is an
// error rather than something to guess at — this decides whether a packet
// gets repeated to the whole network for the next hour.
func skipName(pkt []byte, off int) (int, error) {
	for {
		if off >= len(pkt) {
			return 0, ErrShort
		}
		b := pkt[off]
		switch {
		case b&0xC0 == 0xC0:
			if off+2 > len(pkt) {
				return 0, ErrShort
			}
			return off + 2, nil
		case b&0xC0 != 0:
			return 0, fmt.Errorf("reserved label type 0x%02x at offset %d", b, off)
		case b == 0:
			return off + 1, nil
		default:
			off += 1 + int(b)
		}
	}
}
