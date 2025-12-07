package agent

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"peer-wan/pkg/frr"
	"peer-wan/pkg/model"
	"peer-wan/pkg/policy"
	"peer-wan/pkg/wireguard"
)

// RenderAndWrite generates WireGuard and FRR configs and writes them to outputDir.
// Returns the written file paths.
func RenderAndWrite(outputDir, iface string, node model.Node, peers []model.Peer, privateKey string, asn int) (wgPath, bgpPath string, err error) {
	if err = os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir output: %w", err)
	}
	if node.ListenPort == 0 {
		node.ListenPort = 8082
	}

	peersWithPolicy := append([]model.Peer(nil), peers...)
	hostToLocal := allocateLocalPorts(peersWithPolicy, 30000)
	for i, p := range peersWithPolicy {
		host := endpointHost(p.Endpoint)
		if host == "" {
			continue
		}
		if lp, ok := hostToLocal[host]; ok {
			peersWithPolicy[i].Endpoint = fmt.Sprintf("127.0.0.1:%d", lp)
		}
	}
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
		NodeID:              node.ID,
		EgressPeerID:        node.EgressPeerID,
		PolicyRules:         node.PolicyRules,
		Peers:               peers,
		DefaultRoute:        node.DefaultRoute,
		BypassCIDRs:         node.BypassCIDRs,
		DefaultRouteNextHop: node.DefaultRouteNextHop,
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
	// start wstunnel server/client if available
	wsTunMgr.startAll(hostToLocal, node.ListenPort)
	return wgPath, bgpPath, nil
}

// augmentEgressAllowedIPs ensures the egress peer includes policy prefixes (and resolved domains)
// in its AllowedIPs so WireGuard will actually forward those flows through the tunnel.
// Only the peers that are actual next-hops for a rule will receive those prefixes.
func augmentEgressAllowedIPs(peers *[]model.Peer, node model.Node) {
	// peerID -> list of prefixes that should traverse it
	targets := map[string][]string{}

	// 1) collect per-rule prefixes to the rule's next hop
	for _, pr := range node.PolicyRules {
		pfx := policy.Expand(pr)
		if len(pfx) == 0 {
			continue
		}
		target := pr.ViaNode
		if len(pr.Path) > 0 {
			target = pr.Path[0]
		}
		if target == "" {
			continue
		}
		targets[target] = append(targets[target], pfx...)
	}

	// 2) default route: add 0/0 only to the configured egress peer
	if node.DefaultRoute && node.EgressPeerID != "" {
		targets[node.EgressPeerID] = append(targets[node.EgressPeerID], "0.0.0.0/0")
	}

	for i, p := range *peers {
		want, ok := targets[p.ID]
		if !ok {
			continue
		}
		exist := map[string]struct{}{}
		for _, ip := range p.AllowedIPs {
			exist[ip] = struct{}{}
		}
		for _, pref := range want {
			if _, ok := exist[pref]; ok {
				continue
			}
			p.AllowedIPs = append(p.AllowedIPs, pref)
			exist[pref] = struct{}{}
		}
		(*peers)[i] = p
	}
}

// policyPrefixes converts policy rules into /32 prefixes (for IPs) resolving domains once.
func policyPrefixes(rules []model.PolicyRule) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, pr := range rules {
		for _, pfx := range policy.Expand(pr) {
			if _, ok := seen[pfx]; ok {
				continue
			}
			seen[pfx] = struct{}{}
			out = append(out, pfx)
		}
	}
	return out
}

// applyStaticRoutes installs policy/default routes into kernel so traffic matches immediately.
// Best effort: requires ip command available and sufficient permissions.
func applyStaticRoutes(plan model.Plan, iface string) error {
	primaryGW, primaryDev := detectPrimaryRoute()
	if primaryGW == "" || primaryDev == "" {
		log.Printf("warning: primary route not detected, local bypass may still hit wg")
	} else {
		log.Printf("primary route detected: gw=%s dev=%s", primaryGW, primaryDev)
	}

	// ensure per-peer prefixes are routable even with custom policy tables (e.g., table 52)
	if err := syncPeerRoutes(plan.Peers, iface); err != nil {
		log.Printf("sync peer routes failed: %v", err)
	}

	// track rule hashes for cleanup of stale records
	ruleHashes := map[string]struct{}{}

	apply := func(prefix, nh string) error {
		if prefix == "" || nh == "" {
			return nil
		}
		nhIP := strings.Split(nh, "/")[0]
		// ensure next-hop host route exists (scope link) so kernel accepts it
		if out, err := exec.Command("ip", "route", "replace", nhIP+"/32", "dev", iface, "scope", "link").CombinedOutput(); err != nil {
			log.Printf("ensure nexthop %s link route failed: %v (%s)", nhIP, err, string(out))
		}
		cmds := [][]string{
			{"ip", "route", "replace", prefix, "via", nhIP, "dev", iface},
			{"ip", "route", "replace", prefix, "via", nhIP, "dev", iface, "table", "100"},
		}
		for _, args := range cmds {
			if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
				return fmt.Errorf("%s: %v (%s)", strings.Join(args, " "), err, string(out))
			}
		}
		return nil
	}
	// 默认路由（可选）
	if plan.DefaultRoute {
		targetPeer := plan.DefaultRouteNextHop
		if targetPeer == "" {
			targetPeer = plan.EgressPeerID
		}
		if nh := frrOverlayForPeer(targetPeer, plan.Peers); nh != "" {
			nhIP := strings.Split(nh, "/")[0]
			_ = exec.Command("ip", "route", "replace", "default", "via", nhIP, "dev", iface, "table", "100").Run()
			bypass := plan.BypassCIDRs
			for _, c := range bypass {
				_ = exec.Command("ip", "rule", "add", "from", c, "lookup", "main", "priority", "100").Run()
			}
			_ = exec.Command("ip", "rule", "add", "priority", "200", "lookup", "100").Run()
			log.Printf("default route via %s table100; bypass=%v", nhIP, bypass)
		}
	}
	// 策略前缀
	for _, pr := range plan.PolicyRules {
		if !pr.Validate() {
			continue
		}
		ruleHash := hashRule(pr)
		ruleHashes[ruleHash] = struct{}{}
		pfxList := policy.Expand(pr)
		// 本地直出：对匹配前缀加规则走主路由，避免兜底表100
		if pr.ViaNode == "local" || pr.ViaNode == "main" {
			for _, pfx := range pfxList {
				if out, err := exec.Command("ip", "rule", "add", "to", pfx, "lookup", "main", "priority", "150").CombinedOutput(); err != nil {
					log.Printf("apply policy local rule %s failed: %v (%s)", pfx, err, string(out))
				}
				if primaryGW != "" && primaryDev != "" {
					// 明确在主路由表里放一条非 wg 下一跳，避免默认表被 wg 覆盖
					if out, err := exec.Command("ip", "route", "replace", pfx, "via", primaryGW, "dev", primaryDev).CombinedOutput(); err != nil {
						log.Printf("apply policy local route %s via %s dev %s failed: %v (%s)", pfx, primaryGW, primaryDev, err, string(out))
					}
				}
				recordPolicyOp(ruleHash, "apply_rule", "local main rule "+pfx)
				log.Printf("policy local route %s -> main (bypass default)", pfx)
			}
			continue
		}
		nextID := pr.ViaNode
		if len(pr.Path) > 0 {
			nextID = pr.Path[0]
		}
		nextHop := frrOverlayForPeer(nextID, plan.Peers)
		if nextHop == "" {
			continue
		}
		log.Printf("policy rule applying: prefix=%s domains=%v via=%s path=%v nextHop=%s targets=%d", pr.Prefix, pr.Domains, pr.ViaNode, pr.Path, nextHop, len(pfxList))
		for _, pfx := range pfxList {
			if err := apply(pfx, nextHop); err != nil {
				log.Printf("apply policy route %s -> %s failed: %v", pfx, nextHop, err)
			} else {
				log.Printf("applied policy route %s -> %s (main+table100)", pfx, nextHop)
				recordPolicyOp(ruleHash, "apply_route", pfx+" via "+nextHop)
				// Add a policy rule to prefer main for this prefix (before other rules like table 52)
				if out, err := exec.Command("ip", "rule", "add", "to", pfx, "lookup", "main", "priority", "140").CombinedOutput(); err != nil && !strings.Contains(string(out), "File exists") {
					log.Printf("apply policy rule %s -> main failed: %v (%s)", pfx, err, string(out))
				}
			}
		}
	}
	purgeMissingHashes(ruleHashes)
	// flush route cache so new rules/routes take effect immediately
	_ = exec.Command("ip", "route", "flush", "cache").Run()

	return nil
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

func containsCIDR(list []string, cidr string) bool {
	for _, v := range list {
		if v == cidr {
			return true
		}
	}
	return false
}

// syncPeerRoutes ensures allowed prefixes of peers are present in main and table 52,
// and prunes stale entries in table 52. This keeps multi-hop overlay reachability working
// even when policy routing directs lookups to non-main tables.
func syncPeerRoutes(peers []model.Peer, iface string) error {
	if iface == "" {
		iface = "wg0"
	}
	desired := map[string]struct{}{}
	for _, p := range peers {
		for _, pref := range p.AllowedIPs {
			if pref == "" || pref == "0.0.0.0/0" {
				continue
			}
			if _, ipNet, err := net.ParseCIDR(pref); err != nil || ipNet.IP.To4() == nil {
				continue
			}
			desired[pref] = struct{}{}
		}
	}
	ensure := func(table string) error {
		for pref := range desired {
			args := []string{"ip", "route", "replace", pref, "dev", iface}
			if table != "main" {
				args = append(args, "table", table)
			}
			if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
				return fmt.Errorf("%s: %v (%s)", strings.Join(args, " "), err, string(out))
			}
		}
		return nil
	}
	if err := ensure("main"); err != nil {
		return err
	}
	if err := ensure("52"); err != nil {
		return err
	}

	// prune stale routes in table 52 for this iface
	out, err := exec.Command("ip", "route", "show", "table", "52", "dev", iface).Output()
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			pref := fields[0]
			if _, ok := desired[pref]; ok {
				continue
			}
			// only clean 10.0.0.0/8-style overlay to avoid touching unrelated routes
			if !strings.HasPrefix(pref, "10.") {
				continue
			}
			_ = exec.Command("ip", "route", "del", pref, "dev", iface, "table", "52").Run()
		}
	}
	_ = exec.Command("ip", "route", "flush", "cache").Run()
	return nil
}

// detectPrimaryCIDR best-effort: find primary interface CIDR for default route.
func detectPrimaryCIDR() string {
	out, err := exec.Command("sh", "-c", "ip route get 1.1.1.1 | awk '/dev/{print $5}'").Output()
	if err != nil {
		return ""
	}
	iface := strings.TrimSpace(string(out))
	if iface == "" {
		return ""
	}
	j, err := exec.Command("sh", "-c", "ip -j addr show dev "+iface+" | jq -r '.[0].addr_info[] | select(.family==\"inet\") | .local+\"/\"+.prefixlen'").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(j)), "\n")
	if len(lines) > 0 && lines[0] != "" {
		return lines[0]
	}
	return ""
}

// detectPrimaryRoute returns the first non-WireGuard default route (gw, dev).
func detectPrimaryRoute() (string, string) {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "", ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// expected: default via <gw> dev <dev> ...
		if fields[0] != "default" {
			continue
		}
		var gw, dev string
		for i := 1; i < len(fields)-1; i++ {
			if fields[i] == "via" {
				gw = fields[i+1]
			}
			if fields[i] == "dev" {
				dev = fields[i+1]
			}
		}
		if strings.HasPrefix(dev, "wg") {
			continue
		}
		if gw != "" && dev != "" {
			return gw, dev
		}
	}
	return "", ""
}
