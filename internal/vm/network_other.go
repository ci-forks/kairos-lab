//go:build !darwin && !linux

package vm

// There is no Wi-Fi question to answer off macOS and Linux: this package
// knows no way to ask such a host what kind of radio a device is, and a
// guess would put a caveat about access points in front of a user whose
// interface may be ordinary Ethernet.
//
// The symbol exists at all so that network_notdarwin.go, which is !darwin
// rather than !darwin && !linux, compiles on every GOOS this Go toolchain
// supports rather than only on the two the project ships. See ipaddr_other.go,
// which exists for the same reason.
func isWiFiIface(_ string) bool { return false }
