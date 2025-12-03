package agent

import (
	"fmt"
	"os"
	"path/filepath"

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

	wgConf, err := wireguard.RenderConfig(iface, node, peers, privateKey)
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
	return wgPath, bgpPath, nil
}
