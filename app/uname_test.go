package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"testing"

	"github.com/dirty-go/foundation/errors"
)

func ipNet(s string) *net.IPNet {
	ip, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	n.IP = ip
	return n
}

func mac(s string) net.HardwareAddr {
	hw, err := net.ParseMAC(s)
	if err != nil {
		panic(err)
	}
	return hw
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("rand broken") }

// wantHash mirrors hashID's formula independently of it.
func wantHash(kind, value string) string {
	sum := sha256.Sum256([]byte("dirty-go/foundation/instance-id\x00" + kind + ":" + value))
	return hex.EncodeToString(sum[:16])
}

func TestHashIDKindSeparation(t *testing.T) {
	const v = "aa:bb:cc:dd:ee:ff"
	if hashID("mac", v) == hashID("hostname", v) {
		t.Error(`hashID("mac", v) == hashID("hostname", v), want kinds to separate`)
	}
	if got, want := hashID("mac", v), wantHash("mac", v); got != want {
		t.Errorf("hashID = %s, want %s", got, want)
	}
}

func TestInstanceID(t *testing.T) {
	const (
		macA = "aa:aa:aa:aa:aa:aa"
		macB = "bb:bb:bb:bb:bb:bb"
	)
	up := net.FlagUp
	ifaces := func(i ...netIface) func() ([]netIface, error) {
		return func() ([]netIface, error) { return i, nil }
	}
	host := func(h string, err error) func() (string, error) {
		return func() (string, error) { return h, err }
	}
	random := func() string { return "random" }

	tests := []struct {
		name string
		src  idSource
		want string
	}{
		{
			name: "IPv4 owner beats earlier interface without IPv4",
			src: idSource{
				interfaces: ifaces(
					netIface{Flags: up, HardwareAddr: mac(macA), Addrs: []net.Addr{ipNet("fe80::1/64")}},
					netIface{Flags: up, HardwareAddr: mac(macB), Addrs: []net.Addr{ipNet("10.0.0.1/24")}},
				),
				hostname: host("h", nil), randomID: random,
			},
			want: wantHash("mac", macB),
		},
		{
			name: "loopback IPv4 is skipped",
			src: idSource{
				interfaces: ifaces(
					netIface{Flags: up | net.FlagLoopback, Addrs: []net.Addr{ipNet("127.0.0.1/8")}},
					netIface{Flags: up, HardwareAddr: mac(macB), Addrs: []net.Addr{&net.IPAddr{IP: net.ParseIP("10.0.0.1")}}},
				),
				hostname: host("h", nil), randomID: random,
			},
			want: wantHash("mac", macB),
		},
		{
			name: "down IPv4 owner is skipped",
			src: idSource{
				interfaces: ifaces(
					netIface{HardwareAddr: mac(macA), Addrs: []net.Addr{ipNet("10.0.0.1/24")}},
					netIface{Flags: up, HardwareAddr: mac(macB), Addrs: []net.Addr{ipNet("10.0.0.2/24")}},
				),
				hostname: host("h", nil), randomID: random,
			},
			want: wantHash("mac", macB),
		},
		{
			name: "link-local IPv4 owner is skipped",
			src: idSource{
				interfaces: ifaces(
					netIface{Flags: up, HardwareAddr: mac(macA), Addrs: []net.Addr{ipNet("169.254.1.1/16")}},
					netIface{Flags: up, HardwareAddr: mac(macB), Addrs: []net.Addr{ipNet("192.168.1.2/24")}},
				),
				hostname: host("h", nil), randomID: random,
			},
			want: wantHash("mac", macB),
		},
		{
			name: "IPv4 owner without MAC falls back to first up MAC",
			src: idSource{
				interfaces: ifaces(
					netIface{Flags: up, Addrs: []net.Addr{ipNet("10.8.0.2/24")}},
					netIface{Flags: up, HardwareAddr: mac(macA)},
				),
				hostname: host("h", nil), randomID: random,
			},
			want: wantHash("mac", macA),
		},
		{
			name: "MAC-less IPv4 owner is skipped for a later owner with a MAC",
			src: idSource{
				interfaces: ifaces(
					netIface{Flags: up, Addrs: []net.Addr{ipNet("10.8.0.2/24")}}, // tun
					netIface{Flags: up, HardwareAddr: mac(macB)},                 // bridge, no IPv4
					netIface{Flags: up, HardwareAddr: mac(macA), Addrs: []net.Addr{ipNet("192.168.1.5/24")}},
				),
				hostname: host("h", nil), randomID: random,
			},
			want: wantHash("mac", macA),
		},
		{
			name: "no IPv4 falls back to first up non-loopback MAC",
			src: idSource{
				interfaces: ifaces(
					netIface{Flags: up | net.FlagLoopback, HardwareAddr: mac(macB)},
					netIface{HardwareAddr: mac(macB)}, // down
					netIface{Flags: up},
					netIface{Flags: up, HardwareAddr: mac(macA), Addrs: []net.Addr{ipNet("fe80::1/64")}},
				),
				hostname: host("h", nil), randomID: random,
			},
			want: wantHash("mac", macA),
		},
		{
			name: "lister error falls back to hostname",
			src: idSource{
				interfaces: func() ([]netIface, error) { return nil, errors.New("x") },
				hostname:   host("myhost", nil), randomID: random,
			},
			want: wantHash("hostname", "myhost"),
		},
		{
			name: "no MAC falls back to hostname",
			src: idSource{
				interfaces: ifaces(netIface{Flags: up, Addrs: []net.Addr{ipNet("10.0.0.1/24")}}),
				hostname:   host("myhost", nil), randomID: random,
			},
			want: wantHash("hostname", "myhost"),
		},
		{
			name: "hostname equal to a MAC string hashes differently",
			src:  idSource{interfaces: ifaces(), hostname: host(macA, nil), randomID: random},
			want: wantHash("hostname", macA),
		},
		{
			name: "hostname error falls back to random as is",
			src:  idSource{interfaces: ifaces(), hostname: host("myhost", errors.New("x")), randomID: random},
			want: "random",
		},
		{
			name: "empty hostname falls back to random as is",
			src:  idSource{interfaces: ifaces(), hostname: host("", nil), randomID: random},
			want: "random",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := instanceID(tt.src); got != tt.want {
				t.Errorf("instanceID = %q, want %q", got, tt.want)
			}
		})
	}
	if wantHash("hostname", macA) == wantHash("mac", macA) {
		t.Error("hostname and MAC with the same value hash equal")
	}
}

func TestInstanceIDZeroSource(t *testing.T) {
	if got := instanceID(idSource{}); !isInstanceID(got) {
		t.Errorf("instanceID(idSource{}) = %q, want 32 hex chars", got)
	}
}

func TestInstanceIDRandomStable(t *testing.T) {
	src := idSource{
		hostname: func() (string, error) { return "", nil },
		randomID: newRandomID(bytes.NewReader(bytes.Repeat([]byte{0xab}, 32))),
	}
	got := instanceID(src)
	if len(got) != 32 {
		t.Fatalf("len(%q) = %d, want 32", got, len(got))
	}
	if _, err := hex.DecodeString(got); err != nil {
		t.Errorf("%q is not hex: %v", got, err)
	}
	if again := instanceID(src); again != got {
		t.Errorf("second call = %q, want %q", again, got)
	}
}

func TestNewRandomIDReaderError(t *testing.T) {
	id := newRandomID(errReader{})
	got := id()
	if !isInstanceID(got) {
		t.Fatalf("random ID after reader error = %q, want 32 hex chars", got)
	}
	if again := id(); again != got {
		t.Errorf("second call = %q, want %q", again, got)
	}
}

func TestNewRandomIDShortRead(t *testing.T) {
	if got := newRandomID(bytes.NewReader([]byte{1, 2}))(); !isInstanceID(got) || got == hex.EncodeToString([]byte{1, 2}) {
		t.Errorf("short read gave %q, want 32-hex pid/time fallback", got)
	}
}
