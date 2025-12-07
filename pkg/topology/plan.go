package topology

import (
	"sort"

	"peer-wan/pkg/model"
)

// BuildPeerPlan derives a simple full-mesh peer list for the target node.
// It picks the first endpoint of each other node and only includes that node's
// own overlay/CIDRs as AllowedIPs (others are redundant and can break wg routing).
// Later this can evolve into intent-based or latency-aware plans.
func BuildPeerPlan(targetID string, nodes []model.Node, health map[string]model.HealthReport) []model.Peer {
	type scored struct {
		peer  model.Peer
		score int // lower is better (latency)
	}
	var peers []scored
	for _, n := range nodes {
		if n.ID == targetID {
			continue
		}
		if len(n.CIDRs) == 0 || n.PublicKey == "" {
			continue
		}
		score := 100000 // default high latency
		if h, ok := health[n.ID]; ok {
			if h.Status == "down" {
				continue // skip unhealthy
			}
			// use min latency if available
			for _, ms := range h.LatencyMs {
				if ms < score {
					score = ms
				}
			}
			// if FRR state present and not Established, deprioritize
			for _, st := range h.FRRState {
				if st != "Established" {
					score += 10000
				}
			}
		}
		endpoint := ""
		if len(n.Endpoints) > 0 {
			endpoint = n.Endpoints[0]
		}
		allowed := make([]string, 0, len(n.CIDRs)+1)
		seen := make(map[string]bool, len(n.CIDRs)+1)
		if n.OverlayIP != "" {
			allowed = append(allowed, n.OverlayIP)
			seen[n.OverlayIP] = true
		}
		for _, cidr := range n.CIDRs {
			if !seen[cidr] {
				allowed = append(allowed, cidr)
				seen[cidr] = true
			}
		}
		peers = append(peers, scored{peer: model.Peer{
			ID:         n.ID,
			PublicKey:  n.PublicKey,
			Endpoint:   endpoint,
			AllowedIPs: allowed,
			Keepalive:  25,
		}, score: score})
	}
	sort.SliceStable(peers, func(i, j int) bool {
		return peers[i].score < peers[j].score
	})
	out := make([]model.Peer, 0, len(peers))
	for _, p := range peers {
		out = append(out, p.peer)
	}
	return out
}
