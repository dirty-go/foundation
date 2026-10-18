package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// netIface is the subset of net.Interface that instanceID needs, with its
// addresses already resolved so tests don't depend on the host's interfaces.
type netIface struct {
	Flags        net.Flags
	HardwareAddr net.HardwareAddr
	Addrs        []net.Addr
}

// idSource holds the inputs instanceID chooses from.
type idSource struct {
	interfaces func() ([]netIface, error)
	hostname   func() (string, error)
	randomID   func() string
}

// sysSource reads the host. Its randomID is generated once per process.
var sysSource = idSource{
	interfaces: sysInterfaces,
	hostname:   os.Hostname,
	randomID:   newRandomID(rand.Reader),
}

// sysInstanceID is the instance ID source used by Init. It never fails
// today; the error keeps Init's error path for future sources.
func sysInstanceID() (string, error) { return instanceID(sysSource), nil }

func sysInterfaces() ([]netIface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]netIface, 0, len(ifs))
	for _, ifc := range ifs {
		addrs, _ := ifc.Addrs() // an interface without readable addrs can still supply a MAC
		out = append(out, netIface{Flags: ifc.Flags, HardwareAddr: ifc.HardwareAddr, Addrs: addrs})
	}
	return out, nil
}

// idDomain domain-separates hashed instance IDs from other SHA-256 uses. The
// hash is unsalted: every service using this library on the same host gets
// the same InstanceID, so IDs are linkable across those services.
const idDomain = "dirty-go/foundation/instance-id\x00"

// hashID returns the lowercase hex of the first 16 bytes of
// sha256(idDomain + kind + ":" + value). kind ("mac", "hostname") keeps a
// hostname from colliding with a MAC string.
func hashID(kind, value string) string {
	sum := sha256.Sum256([]byte(idDomain + kind + ":" + value))
	return hex.EncodeToString(sum[:16])
}

// instanceID picks a non-empty, 32-hex-char instance ID, in order:
//  1. hashID("mac", …) of the first up interface with a MAC owning a
//     non-loopback, non-link-local IPv4 address;
//  2. hashID("mac", …) of the first up, non-loopback interface with a MAC;
//  3. hashID("hostname", …) of the hostname;
//  4. src.randomID(), as is (random, so not identifying).
func instanceID(src idSource) string {
	var ifaces []netIface
	if src.interfaces != nil {
		ifaces, _ = src.interfaces() // nil on error: fall through to the next source
	}
	if ifc, ok := ipv4Owner(ifaces); ok {
		return hashID("mac", ifc.HardwareAddr.String())
	}
	for _, ifc := range ifaces {
		if isUp(ifc) && ifc.Flags&net.FlagLoopback == 0 && len(ifc.HardwareAddr) > 0 {
			return hashID("mac", ifc.HardwareAddr.String())
		}
	}
	if src.hostname != nil {
		if h, err := src.hostname(); err == nil && h != "" {
			return hashID("hostname", h)
		}
	}
	if src.randomID != nil {
		return src.randomID()
	}
	return newRandomID(rand.Reader)()
}

// ipv4Owner returns the first up interface that has a MAC and a non-loopback,
// non-link-local (169.254/16) IPv4 address. Interfaces without a MAC (tun,
// wireguard) are skipped, so a later eth0 wins over a VPN device.
func ipv4Owner(ifaces []netIface) (netIface, bool) {
	for _, ifc := range ifaces {
		if !isUp(ifc) || len(ifc.HardwareAddr) == 0 {
			continue
		}
		for _, a := range ifc.Addrs {
			ip := addrIP(a)
			if ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				return ifc, true
			}
		}
	}
	return netIface{}, false
}

func isUp(ifc netIface) bool { return ifc.Flags&net.FlagUp != 0 }

func addrIP(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	}
	return nil
}

// newRandomID returns a func yielding the same ID on every call: 16 bytes
// from r, hex-encoded (32 chars). If r fails (only possible for an injected
// reader: crypto/rand.Reader doesn't return errors on Go >= 1.24) it falls
// back to a non-random 32-hex-char ID from the pid and the time of first call.
func newRandomID(r io.Reader) func() string {
	return sync.OnceValue(func() string {
		b := make([]byte, 16)
		if _, err := io.ReadFull(r, b); err == nil {
			return hex.EncodeToString(b)
		}
		return fmt.Sprintf("%016x%016x", os.Getpid(), time.Now().UnixNano())
	})
}
