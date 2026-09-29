// Tests for the state a removed network mode leaves behind.
//
// See the note at the top of network_linux_test.go for why there is no build
// tag here and what newFakeHost replaces. The _linux_test.go suffix is what
// gives this file the same constraint as the code it tests, and it is load
// bearing: every symbol below -- linuxNetworkPreflight, CleanupLinuxBridge,
// fakeHost -- exists only in the Linux build of the package, so without it
// the macOS leg fails to compile.
package vm

import (
	"slices"
	"strings"
	"testing"

	"github.com/kairos-io/kairos-lab/internal/state"
)

// libvirtBridge is the bridge `--network virbr` used to attach its tap to. The
// mode is gone, but a state.json that names this bridge is not: the file
// survives the upgrade that removed the mode, and every path below reads
// BridgeName out of it.
const libvirtBridge = "virbr0"

// virbrTap is the tap name that mode recorded next to it: the value of the
// DefaultVirbrTapName constant as it stood in the release before the mode was
// removed. It is spelled out rather than referenced because the constant it
// came from is gone, and it is the literal string that is in the state.json
// files this test is about. 15 bytes, so it passes the stored-name validator
// -- an upgrade must reach the mode gate below, not be refused before it.
const virbrTap = "kairoslab-vtap0"

// virbrState is the state.json `--network virbr` wrote: its own tap on
// libvirt's bridge, CreatedByKairosLab set -- true of the tap, never of the
// bridge -- and a mode no build still offers.
func virbrState() *state.State {
	st := &state.State{}
	st.Network.Mode = "virbr"
	st.Network.BridgeName = libvirtBridge
	st.Network.TapName = virbrTap
	st.Network.CleanupRequired = true
	st.Network.CreatedByKairosLab = true
	return st
}

// seedLibvirtHost puts libvirt's default network on the fake host, with a
// guest tap of its own on the bridge so that a teardown reaching it would
// have something to reconnect.
func seedLibvirtHost(h *fakeHost) {
	h.bridges[libvirtBridge] = true
	h.links[virbrTap] = true
	h.slaves[libvirtBridge] = []string{virbrTap, "vnet0"}
}

// assertLibvirtUntouched fails with the offending command when anything the
// run issued names libvirt's bridge. Naming it in ANY argv is the failure:
// every command these paths build from the resolved bridge name is
// destructive to it -- `nmcli connection delete`, `nmcli connection down`,
// `ip link delete` -- and there is no read-only one to allow through.
func assertLibvirtUntouched(t *testing.T, h *fakeHost) {
	t.Helper()
	for _, line := range h.lines() {
		if slices.Contains(strings.Fields(line), libvirtBridge) {
			t.Errorf("a run over virbr state issued %q, which is libvirt's own bridge", line)
		}
	}
}

// A state.json left by `--network virbr` must not decide which bridge the
// surviving modes tear down.
//
// The mode recorded libvirt's bridge in BridgeName. That field is read back by
// the bridged and shared paths, and each of them goes on to DELETE what it
// resolved: the start preflight as stale-resource cleanup, `reset` and
// `cleanup` as teardown. Inheriting virbr0 takes the default network down for
// every libvirt VM on the host, and the bridged path then builds its
// replacement and enslaves the host uplink to it.
//
// Upgrading is the whole path: nothing writes this state any more, and
// nothing rewrites it either, so the file sits there until a start in one of
// the modes that is left reads it.
func TestVirbrStateNeverNamesTheBridgeToDestroy(t *testing.T) {
	// Both start paths resolve the bridge with resolveLinuxBridgeName and
	// then hand it to linuxNetworkPreflight, so the two steps are driven here
	// the way PrepareLinuxBridge and PrepareLinuxShared drive them.
	for _, mode := range []string{"shared", "bridged"} {
		t.Run("a "+mode+" start does not tear down libvirt's bridge", func(t *testing.T) {
			h := newFakeHost(t)
			seedLibvirtHost(h)

			st := virbrState()
			bridge, err := resolveLinuxBridgeName(st)
			if err != nil {
				t.Fatalf("resolving the bridge over virbr state: %v", err)
			}
			if bridge != DefaultBridgeName {
				t.Errorf("the start resolved bridge %q, want the default %q", bridge, DefaultBridgeName)
			}
			if err := linuxNetworkPreflight(st, t.TempDir(), mode, bridge, TapNameForIndex(0), TapConnNameForIndex(bridge, 0), false); err != nil {
				t.Fatalf("preflight over virbr state: %v", err)
			}
			assertLibvirtUntouched(t, h)
		})
	}

	// `reset` and `cleanup` reach CleanupLinuxBridge without passing through
	// the preflight, and its only gate is CreatedByKairosLab -- which the
	// virbr path set.
	t.Run("reset does not delete libvirt's bridge", func(t *testing.T) {
		h := newFakeHost(t)
		seedLibvirtHost(h)

		if err := CleanupLinuxBridge(virbrState(), state.VM{}, false); err != nil {
			t.Fatalf("CleanupLinuxBridge over virbr state: %v", err)
		}
		assertLibvirtUntouched(t, h)
		if !h.bridges[libvirtBridge] {
			t.Error("libvirt's bridge was deleted by a teardown that did not create it")
		}
	})

	t.Run("stale cleanup does not delete libvirt's bridge", func(t *testing.T) {
		h := newFakeHost(t)
		seedLibvirtHost(h)

		if err := CleanupStaleNetworkResources(virbrState()); err != nil {
			t.Fatalf("CleanupStaleNetworkResources over virbr state: %v", err)
		}
		assertLibvirtUntouched(t, h)
		if !h.bridges[libvirtBridge] {
			t.Error("libvirt's bridge was deleted as a stale kairos-lab resource")
		}
	})

	// The reporter that decides whether the user is ASKED to clean up must
	// not point at libvirt's bridge either: the consent prompt names what it
	// found, and answering yes is what runs the teardown above.
	t.Run("libvirt's bridge is not reported as a stale kairos-lab resource", func(t *testing.T) {
		h := newFakeHost(t)
		seedLibvirtHost(h)

		st := virbrState()
		st.Network.CreatedByKairosLab = false
		if HasStaleNetworkResources(st) {
			t.Error("libvirt's bridge is reported as a leftover kairos-lab resource")
		}
	})
}

// The name is still read back for the modes that write it: a resumed run must
// reuse the bridge it created, not build a second one beside it. This is the
// half of managedBridgeName that a mode gate could break.
func TestManagedBridgeNameKeepsTheNameTheseModesWrote(t *testing.T) {
	for _, mode := range []string{"bridged", "shared"} {
		t.Run(mode, func(t *testing.T) {
			n := state.Network{Mode: mode, BridgeName: "kairoslab7"}
			if bridge := managedBridgeName(n); bridge != "kairoslab7" {
				t.Errorf("managedBridgeName dropped the bridge %s recorded: got %q", mode, bridge)
			}
		})
	}

	// A record that names no bridge falls back to the default rather than to
	// the empty string, whatever mode wrote it.
	t.Run("a mode that recorded no bridge", func(t *testing.T) {
		if bridge := managedBridgeName(state.Network{Mode: "shared"}); bridge != DefaultBridgeName {
			t.Errorf("managedBridgeName() = %q, want the default %q", bridge, DefaultBridgeName)
		}
	})

	// Every other mode, and the empty mode of a state.json written before
	// the field existed, resolves to the default.
	for _, mode := range []string{"", "virbr", "user", "unknown"} {
		t.Run("mode "+mode, func(t *testing.T) {
			n := state.Network{Mode: mode, BridgeName: libvirtBridge, TapName: virbrTap}
			if bridge := managedBridgeName(n); bridge != DefaultBridgeName {
				t.Errorf("managedBridgeName() = %q for mode %q, want the default %q", bridge, mode, DefaultBridgeName)
			}
		})
	}
}

// A malformed stored name is rejected whatever mode recorded it: validation
// asks whether a value is safe to hand to root, the mode gate asks whether
// this run owns it, and answering only the second leaves a bad name in the
// file unexamined until the mode that uses it reads it back.
func TestResolveLinuxBridgeNameValidatesWhateverModeWroteIt(t *testing.T) {
	st := virbrState()
	st.Network.BridgeName = "kairoslab0; rm -rf /"
	if _, err := resolveLinuxBridgeName(st); err == nil {
		t.Error("a malformed bridge name was accepted because the mode does not manage its own bridge")
	}
}

// st.Network is shared by every VM in a config dir, and runStart writes
// st.Network.Mode on every start -- `--network user` included. So a user-mode
// start beside a live shared VM leaves Mode == "user" next to a bridge
// kairos-lab really does own, and the mode gate above then declines to read
// the stored name.
//
// That is harmless for every state this tool can produce on its own, which is
// what this pins. BridgeName has no CLI surface: PrepareLinuxBridge and
// PrepareLinuxShared write back whatever resolveLinuxBridgeName handed them,
// and that is DefaultBridgeName unless the file already held something else.
// So the name the gate falls back to and the name it declined to read are the
// same string, and the clobbered mode costs nothing.
//
// A bridge name someone put in state.json by hand is the case where it does
// cost something: after a user-mode start in the same config dir, a later
// teardown resolves the default instead of that name and leaves it standing.
// It leaks a bridge rather than destroying one that is not ours, which is the
// direction this gate exists to fail in.
func TestAUserModeStartDoesNotChangeWhichBridgeATeardownFinds(t *testing.T) {
	sharedRun := state.Network{
		Mode:               "shared",
		BridgeName:         DefaultBridgeName,
		CreatedByKairosLab: true,
	}
	want := managedBridgeName(sharedRun)

	clobbered := sharedRun
	clobbered.Mode = "user"
	if got := managedBridgeName(clobbered); got != want {
		t.Errorf("a user-mode start moved the teardown's bridge from %q to %q", want, got)
	}
	if want != DefaultBridgeName {
		t.Fatalf("this test assumes a tool-built shared run records %q, got %q", DefaultBridgeName, want)
	}
}
