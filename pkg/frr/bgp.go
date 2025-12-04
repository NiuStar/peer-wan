package frr

import (
	"fmt"
	"net"
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
			fmt.Fprintf(&b, " ip route 0.0.0.0/0 %s\n", stripMask(nextHop))
		}
	}
	for _, pr := range plan.PolicyRules {
		if pr.ViaNode == "" {
			continue
		}
		nh := stripMask(overlayForPeer(pr.ViaNode, plan.Peers))
		if nh == "" {
			continue
		}
		targets := []string{}
		if pr.Prefix != "" {
			pfx := pr.Prefix
			if !strings.Contains(pfx, "/") {
				pfx = pr.Prefix + "/32"
			}
			targets = append(targets, pfx)
		}
		for _, d := range pr.Domains {
			for _, ip := range resolveDomain(d) {
				targets = append(targets, ip+"/32")
			}
		}
		for _, t := range targets {
			fmt.Fprintf(&b, " ip route %s %s\n", t, nh)
		}
	}
	b.WriteString("!\n")

	return BGPConfig{BGPD: b.String()}, nil
}

func stripMask(ip string) string {
	if i := strings.Index(ip, "/"); i > 0 {
		return ip[:i]
	}
	return ip
}

func resolveDomain(domain string) []string {
	out := []string{}
	ipList, err := net.LookupIP(domain)
	if err != nil {
		return out
	}
	for _, ip := range ipList {
		if v4 := ip.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	return out
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
