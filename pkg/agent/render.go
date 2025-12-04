package agent

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"peer-wan/pkg/frr"
	"peer-wan/pkg/model"
	"peer-wan/pkg/wireguard"
)

// RenderAndWrite generates WireGuard and FRR configs and writes them to outputDir.
// Returns the written file paths.
func RenderAndWrite(outputDir, iface string, node model.Node, peers []model.Peer, privateKey string, asn int) (wgPath, bgpPath string, err error) {
	if err = os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir output: %w", err)
	}

	peersWithPolicy := append([]model.Peer(nil), peers...)
	augmentEgressAllowedIPs(&peersWithPolicy, node)

	wgConf, err := wireguard.RenderConfig(iface, node, peersWithPolicy, privateKey)
	if err != nil {
		return "", "", fmt.Errorf("render wireguard: %w", err)
	}
	wgPath = filepath.Join(outputDir, fmt.Sprintf("%s.conf", iface))
	if err = os.WriteFile(wgPath, []byte(wgConf), 0o600); err != nil {
		return "", "", fmt.Errorf("write wireguard config: %w", err)
	}

	neighbors := frr.NeighborOverlayIPs(peers)
	routerID := node.RouterID
	if routerID == "" {
		routerID = node.OverlayIP
	}
	plan := model.Plan{
		NodeID:       node.ID,
		EgressPeerID: node.EgressPeerID,
		PolicyRules:  node.PolicyRules,
		Peers:        peers,
	}
	bgpConf, err := frr.RenderBGP(asn, routerID, iface, neighbors, node.CIDRs, plan)
	if err != nil {
		return wgPath, "", fmt.Errorf("render bgp: %w", err)
	}
	bgpPath = filepath.Join(outputDir, "bgpd.conf")
	if err = os.WriteFile(bgpPath, []byte(bgpConf.BGPD), 0o644); err != nil {
		return wgPath, "", fmt.Errorf("write bgp config: %w", err)
	}
	if err := applyStaticRoutes(plan, iface); err != nil {
		log.Printf("apply static routes failed: %v", err)
	}
	return wgPath, bgpPath, nil
}

// augmentEgressAllowedIPs ensures the egress peer includes policy prefixes (and resolved domains)
// in its AllowedIPs so WireGuard will actually forward those flows through the tunnel.
func augmentEgressAllowedIPs(peers *[]model.Peer, node model.Node) {
	if node.EgressPeerID == "" || len(node.PolicyRules) == 0 {
		return
	}
	prefixes := policyPrefixes(node.PolicyRules)
	if len(prefixes) == 0 {
		return
	}

	for i, p := range *peers {
		if p.ID != node.EgressPeerID {
			continue
		}
		exist := map[string]struct{}{}
		for _, ip := range p.AllowedIPs {
			exist[ip] = struct{}{}
		}
		for _, pref := range prefixes {
			if _, ok := exist[pref]; !ok {
				p.AllowedIPs = append(p.AllowedIPs, pref)
				exist[pref] = struct{}{}
			}
		}
		(*peers)[i] = p
		return
	}
}

// policyPrefixes converts policy rules into /32 prefixes (for IPs) resolving domains once.
func policyPrefixes(rules []model.PolicyRule) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, pr := range rules {
		if !pr.Validate() {
			continue
		}
		if pr.Prefix != "" {
			pfx := pr.Prefix
			if !strings.Contains(pfx, "/") {
				pfx = pr.Prefix + "/32"
			}
			if _, ok := seen[pfx]; !ok {
				out = append(out, pfx)
				seen[pfx] = struct{}{}
			}
		}
		for _, d := range pr.Domains {
			ip := resolveOnce(d)
			if ip == "" {
				continue
			}
			pfx := ip + "/32"
			if _, ok := seen[pfx]; !ok {
				out = append(out, pfx)
				seen[pfx] = struct{}{}
			}
		}
	}
	return out
}

// applyStaticRoutes installs policy/default routes into kernel so traffic matches immediately.
// Best effort: requires ip command available and sufficient permissions.
func applyStaticRoutes(plan model.Plan, iface string) error {
	apply := func(prefix, nh string) error {
		if prefix == "" || nh == "" {
			return nil
		}
		nhIP := strings.Split(nh, "/")[0]
		cmd := exec.Command("ip", "route", "replace", prefix, "via", nhIP, "dev", iface)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ip route %s via %s: %v (%s)", prefix, nhIP, err, string(out))
		}
		return nil
	}
	// default route is not installed automatically to avoid breaking reachability; only policy prefixes below.
	for _, pr := range plan.PolicyRules {
		if !pr.Validate() {
			continue
		}
		nextHop := frrOverlayForPeer(pr.ViaNode, plan.Peers)
		if nextHop == "" {
			continue
		}
		log.Printf("policy rule applying: prefix=%s domains=%v via=%s nextHop=%s", pr.Prefix, pr.Domains, pr.ViaNode, nextHop)
		pfxList := []string{}
		if pr.Prefix != "" {
			pfx := pr.Prefix
			if !strings.Contains(pfx, "/") {
				pfx = pr.Prefix + "/32"
			}
			pfxList = append(pfxList, pfx)
		}
		for _, d := range pr.Domains {
			ip := resolveOnce(d)
			if ip != "" {
				pfxList = append(pfxList, ip+"/32")
			}
		}
		for _, pfx := range pfxList {
			if err := apply(pfx, nextHop); err != nil {
				log.Printf("apply policy route %s -> %s failed: %v", pfx, nextHop, err)
			} else {
				log.Printf("applied policy route %s -> %s", pfx, nextHop)
			}
		}
	}
	return nil
}

// resolveOnce resolves a domain to IPv4 address (best effort, first A record).
func resolveOnce(domain string) string {
	if domain == "" {
		return ""
	}
	cmd := exec.Command("dig", "+short", "A", domain)
	b, err := cmd.Output()
	if err != nil {
		log.Printf("resolve domain %s failed: %v", domain, err)
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for _, l := range lines {
		if ip := net.ParseIP(strings.TrimSpace(l)); ip != nil && ip.To4() != nil {
			log.Printf("resolved domain %s -> %s", domain, ip.String())
			return ip.String()
		}
	}
	log.Printf("resolve domain %s got no IPv4", domain)
	return ""
}

// frrOverlayForPeer mirrors frr.overlayForPeer but is local to avoid import cycle.
func frrOverlayForPeer(id string, peers []model.Peer) string {
	for _, p := range peers {
		if p.ID == id {
			for _, ip := range p.AllowedIPs {
				return ip
			}
		}
	}
	return ""
}
