package vm

import "github.com/kairos-io/kairos-lab/internal/state"

// StaleNetworkResourceNames resolves the bridge, the tap device and the tap
// connection that CleanupStaleNetworkResources acts on for this state:
// managedBridgeName's answer, and index 0's tap names -- see
// HasStaleNetworkResources for why index 0 is the right one to ask about here.
//
// It is one function rather than a rule written out wherever it is needed
// because the two places that needed it had drifted. The stale branch of the
// reset and cleanup plans printed DefaultBridgeName unconditionally and named
// no tap at all, while the teardown it was asking the user to consent to read
// st.Network.BridgeName. With a bridge stored, the plan named a bridge and
// three connections the teardown would leave alone, and the teardown deleted
// connections and links -- including an `ip link delete` of the tap -- that
// the plan had never named.
//
// The defaulting lives in managedBridgeName and not in the caller for the
// same reason: the bridge's fallback used to sit in HasStaleNetworkResources
// and in CleanupStaleNetworkResources, two copies of one rule with no single
// place to read it off. A plan that has to restate the rule to print it is a
// plan that can restate it wrongly, and the mode guard added to that rule
// has to reach the plan and the teardown together or the plan goes back to
// naming what the teardown will not touch.
//
// This resolves names; it says nothing about whether anything of that name is
// on the host. HasStaleNetworkResources is what answers that.
func StaleNetworkResourceNames(st *state.State) (bridge, tapDevice, tapConn string) {
	bridge = managedBridgeName(st.Network)
	return bridge, TapNameForIndex(0), TapConnNameForIndex(bridge, 0)
}

// managedBridgeName resolves the host-level bridge name for a run --
// st.Network.BridgeName, or DefaultBridgeName. Unlike the tap, the bridge is
// not per-VM: every VM in this config dir shares one bridge and gets its own
// tap on it.
//
// The stored name is used ONLY when state was written by a mode that builds a
// kairos-lab bridge of its own. That field is not private to those modes:
// `--network virbr`, which this tool dropped, recorded libvirt's own bridge
// (virbr0) in it, and the state.json it wrote survives the upgrade that
// removed the mode. Every caller of this helper goes on to DELETE the bridge
// it resolves -- the start preflight as stale-resource cleanup, `reset` and
// `cleanup` as teardown -- so inheriting virbr0 takes libvirt's default
// network down for every VM on the host, and on the bridged path enslaves the
// host uplink to the replacement bridge built in its place.
//
// Mode is the right question to ask rather than CreatedByKairosLab, which the
// virbr path also set: the tap it created really was kairos-lab's, the bridge
// under it never was.
func managedBridgeName(n state.Network) string {
	if !modeManagesOwnBridge(n.Mode) || n.BridgeName == "" {
		return DefaultBridgeName
	}
	return n.BridgeName
}

// modeManagesOwnBridge reports whether a run in this network mode creates the
// bridge named in state, and may therefore destroy it.
func modeManagesOwnBridge(mode string) bool {
	return mode == "bridged" || mode == "shared"
}
