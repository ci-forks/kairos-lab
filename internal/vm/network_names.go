package vm

import "github.com/kairos-io/kairos-lab/internal/state"

// StaleNetworkResourceNames resolves the bridge, the tap device and the tap
// connection that CleanupStaleNetworkResources acts on for this state: the
// stored bridge or the default where the field is empty, and index 0's tap
// names -- see HasStaleNetworkResources for why index 0 is the right one to
// ask about here.
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
// The defaulting lives here and not in the caller for the same reason: the
// bridge's fallback used to sit in HasStaleNetworkResources and in
// CleanupStaleNetworkResources, two copies of one rule with no single place
// to read it off. A plan that has to restate the rule to print it is a plan
// that can restate it wrongly.
//
// This resolves names; it says nothing about whether anything of that name is
// on the host. HasStaleNetworkResources is what answers that.
func StaleNetworkResourceNames(st *state.State) (bridge, tapDevice, tapConn string) {
	bridge = st.Network.BridgeName
	if bridge == "" {
		bridge = DefaultBridgeName
	}
	return bridge, TapNameForIndex(0), TapConnNameForIndex(bridge, 0)
}
