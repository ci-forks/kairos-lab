package vm

import (
	"os"
	"slices"
	"testing"
)

// The paths the Wi-Fi question is asked about are the decision, so they are
// pinned here rather than left to the seam every other test replaces --
// exactly as TestNetDeviceExistsStatsTheDeviceEntry pins netDevicePath's.
//
// The mutation this catches is the one that reads plausible:
// /sys/class/net/<name>/type. That entry exists for every device and holds
// ARPHRD_ETHER (1) for a Wi-Fi device in managed mode, which is every Wi-Fi
// device a user bridges onto -- so a detector built on it answers "not Wi-Fi"
// for the whole case this exists for, while a suite that only swapped the
// stat stayed green.
func TestWirelessDevicePathsAsksAboutTheRadioEntries(t *testing.T) {
	orig := statNetDevice
	t.Cleanup(func() { statNetDevice = orig })

	var statted []string
	statNetDevice = func(path string) error {
		statted = append(statted, path)
		return os.ErrNotExist
	}

	if isWiFiIface("wlp2s0") {
		t.Fatal("isWiFiIface answered true for a host where neither entry is there")
	}
	want := []string{"/sys/class/net/wlp2s0/phy80211", "/sys/class/net/wlp2s0/wireless"}
	if !slices.Equal(statted, want) {
		t.Errorf("the stats were asked about %q, want %q -- any other entry answers a different question than \"is this device a radio\"", statted, want)
	}
}

// Either entry on its own is enough, because neither covers every driver: a
// cfg80211 device has phy80211 and a WEXT-only one has only wireless. A
// detector that demanded both would answer false for half the drivers in use,
// and a suite that only ever presented both would not notice.
func TestIsWiFiIfaceAcceptsEitherRadioEntryAlone(t *testing.T) {
	orig := statNetDevice
	t.Cleanup(func() { statNetDevice = orig })

	for _, present := range []string{"/sys/class/net/wlan0/phy80211", "/sys/class/net/wlan0/wireless"} {
		statNetDevice = func(path string) error {
			if path == present {
				return nil
			}
			return os.ErrNotExist
		}
		if !isWiFiIface("wlan0") {
			t.Errorf("isWiFiIface answered false for a device whose only radio entry is %s", present)
		}
	}
}

// An ordinary NIC is the answer the warning depends on being right: a caveat
// about access points printed for a user on Ethernet is noise, and noise is
// what gets a real warning ignored.
func TestIsWiFiIfaceAnswersFalseForAnEthernetDevice(t *testing.T) {
	orig := statNetDevice
	t.Cleanup(func() { statNetDevice = orig })

	statNetDevice = func(path string) error {
		// The device itself is there; neither radio entry under it is.
		if path == "/sys/class/net/enp0s31f6" {
			return nil
		}
		return os.ErrNotExist
	}

	if isWiFiIface("enp0s31f6") {
		t.Error("isWiFiIface answered true for a device with no radio entry, so every Ethernet run gets the Wi-Fi caveat")
	}
}

// This gates a warning and never a refusal, so it fails OPEN: an unreadable
// /sys costs the user a caveat, not the run. That is the opposite of
// refusePreexistingBridgePort's reading of the same kind of error, and the
// difference is what each one gates.
func TestIsWiFiIfaceAnswersFalseWhenTheStatFails(t *testing.T) {
	orig := statNetDevice
	t.Cleanup(func() { statNetDevice = orig })

	statNetDevice = func(string) error { return os.ErrPermission }

	if isWiFiIface("wlp2s0") {
		t.Error("isWiFiIface read a permission error as a radio, so an unreadable /sys would print the caveat for every interface")
	}
}

// An empty name reaches here from a bridged run that named no interface, and
// "" must not be joined into /sys/class/net and statted: /sys/class/net
// itself is a directory that exists, so the stat of a path built from it
// would succeed and call the empty interface a radio.
func TestIsWiFiIfaceAnswersFalseForAnEmptyName(t *testing.T) {
	orig := statNetDevice
	t.Cleanup(func() { statNetDevice = orig })

	statted := false
	statNetDevice = func(string) error {
		statted = true
		return nil
	}

	if isWiFiIface("") {
		t.Error("isWiFiIface answered true for an empty interface name")
	}
	if statted {
		t.Error("isWiFiIface statted a path built from an empty name")
	}
}

// IsWiFiIface is what internal/app calls, and network_notdarwin.go's job is to
// pass the question through rather than answer false itself, which is what it
// did before kairos-io/kairos#5021. Restoring that stub leaves every test
// above green, because none of them goes through the exported name.
func TestIsWiFiIfaceExportedDelegatesToThePlatformDetector(t *testing.T) {
	orig := statNetDevice
	t.Cleanup(func() { statNetDevice = orig })

	statNetDevice = func(path string) error {
		if path == "/sys/class/net/wlp2s0/phy80211" {
			return nil
		}
		return os.ErrNotExist
	}

	if !IsWiFiIface("wlp2s0") {
		t.Error("vm.IsWiFiIface answered false for a radio, so the caveat is unreachable on Linux whatever the app layer does")
	}
}
