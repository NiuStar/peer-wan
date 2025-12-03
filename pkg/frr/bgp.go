package frr

import (
	"fmt"
	"strings"

	"peer-wan/pkg/model"
)

// BGPConfig contains rendered FRR bgpd configuration.
type BGPConfig struct {
	BGPD string
}

// RenderBGP builds a minimal bgpd.conf for iBGP peering across the overlay.
// - localASN: ASN for this node
// - neighbors: map of neighbor overlay IP -> ASN (typically same ASN for iBGP)
// - advertized: list of prefixes to announce
func RenderBGP(localASN int, routerID string, sourceInterface string, neighbors map[string]int, advertised []string, plan model.Plan) (BGPConfig, error) {
	if localASN == 0 {
		localASN = 65000
	}
	if sourceInterface == "" {
		sourceInterface = "wg0"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "router bgp %d\n", localASN)
	if routerID != "" {
		fmt.Fprintf(&b, " bgp router-id %s\n", routerID)
	}
	for ip, asn := range neighbors {
		if asn == 0 {
			asn = localASN
		}
		fmt.Fprintf(&b, " neighbor %s remote-as %d\n", ip, asn)
		fmt.Fprintf(&b, " neighbor %s update-source %s\n", ip, sourceInterface)
	}
	for _, pfx := range advertised {
		fmt.Fprintf(&b, " network %s\n", pfx)
	}
	// policy: default route via egress peer overlay, policy rules as static routes
	if plan.EgressPeerID != "" && len(plan.Peers) > 0 {
		if nextHop := overlayForPeer(plan.EgressPeerID, plan.Peers); nextHop != "" {
			fmt.Fprintf(&b, " ip route 0.0.0.0/0 %s\n", nextHop)
		}
	}
	for _, pr := range plan.PolicyRules {
		if pr.Prefix == "" || pr.ViaNode == "" {
			continue
		}
		pfx := pr.Prefix
		if !strings.Contains(pfx, "/") {
			// default to /32 host route if mask missing
			pfx = pr.Prefix + "/32"
		}
		if nh := overlayForPeer(pr.ViaNode, plan.Peers); nh != "" {
			fmt.Fprintf(&b, " ip route %s %s\n", pfx, nh)
		}
	}
	b.WriteString("!\n")

	return BGPConfig{BGPD: b.String()}, nil
}

func overlayForPeer(id string, peers []model.Peer) string {
	for _, p := range peers {
		if p.ID == id {
			for _, ip := range p.AllowedIPs {
				return ip
			}
		}
	}
	return ""
}

// NeighborOverlayIPs derives neighbor IPs from peers' AllowedIPs by picking the first entry.
// Assumes AllowedIPs contain the overlay /32 of the peer.
func NeighborOverlayIPs(peers []model.Peer) map[string]int {
	res := make(map[string]int)
	for _, p := range peers {
		if len(p.AllowedIPs) == 0 {
			continue
		}
		res[p.AllowedIPs[0]] = 0 // same ASN by default
	}
	return res
}
