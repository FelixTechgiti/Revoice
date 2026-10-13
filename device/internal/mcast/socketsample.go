package mcast

import "errors"

// ErrNoSocket is what a sample reports when there is no listener to ask. It is
// deliberately an error rather than a zero reading: a device whose group socket
// never opened is one this probe cannot measure, and the two readings lead to
// opposite conclusions.
var ErrNoSocket = errors.New("no mDNS group socket to read arrivals from")

// SocketSample adapts a counter of packets from OTHER hosts into the Reading
// the Tracker consumes.
//
// The counter it is given must answer "how many mDNS packets have reached this
// device from somebody else" — never "how many packets matched a rule". That
// distinction is the whole of #384: the firewall's mDNS rule matches on the
// destination address, so this device's own announcements, delivered back up
// the input path by the kernel's multicast loopback, moved the counter the
// probe read. The repair restarted the endpoints, the endpoints announced, and
// the probe read its own echo as the network coming back.
//
// `ok` false means there was nothing to ask, which is an absent sample and not
// a silent one.
func SocketSample(count func() (int64, bool)) func() Reading {
	return func() Reading {
		if count == nil {
			return Reading{Err: ErrNoSocket}
		}
		n, ok := count()
		if !ok {
			return Reading{Err: ErrNoSocket}
		}
		return Reading{Packets: n, Found: true}
	}
}
