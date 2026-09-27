//go:build !darwin

package vm

// The macOS bridge lives on a vmnet interface picked from the host's own
// interfaces; on every other platform there is nothing to resolve, so these
// are no-ops. See network_darwin.go.

func DetectBridgeIfaceCandidates() []string { return nil }

func ValidateBridgeIface(_ string) error { return nil }

func ValidateReviewBridgeIface(_ string) error { return nil }

// IsWiFiIface delegates rather than answering false here, which is what it
// used to do. The warning it gates is not macOS-specific advice -- an access
// point drops frames from a MAC it did not see associate whatever host is
// bridging onto it -- and on Linux the radio is the interface a bridged run
// picks by DEFAULT, since DetectUplinkCandidates takes the default route and
// a laptop's is over Wi-Fi. A detector hardwired to false made the warning
// unreachable on the platform that needs it most, while WiFiBridgeWarning
// right below was already shared (kairos-io/kairos#5021).
//
// The platform files supply only the detector, exactly as ipaddr.go's
// lookupLeaseFile takes only the parser: the message and the decision to warn
// rather than refuse are settled once, here and in wifiBridgeWarning, so the
// two platforms cannot drift apart about either.
func IsWiFiIface(iface string) bool { return isWiFiIface(iface) }

func WiFiBridgeWarning(iface string) string { return wifiBridgeWarning(iface) }

func BridgeIfaceStatus(_ string) string { return "" }
