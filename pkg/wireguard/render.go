package wireguard

import (
	"fmt"
	"strings"

	"peer-wan/pkg/model"
)

// RenderConfig produces a wg-quick compatible config string for an interface.
// It uses the node's OverlayIP as Address and ListenPort (if provided) and builds
// peers from the provided peer list.
func RenderConfig(iface string, node model.Node, peers []model.Peer, privateKey string) (string, error) {
	if iface == "" {
		iface = "wg0"
	}
	var b strings.Builder
	b.WriteString("[Interface]\n")
	if node.OverlayIP != "" {
		fmt.Fprintf(&b, "Address = %s\n", node.OverlayIP)
	}
	if node.ListenPort > 0 {
		fmt.Fprintf(&b, "ListenPort = %d\n", node.ListenPort)
	}
	if privateKey != "" {
		fmt.Fprintf(&b, "PrivateKey = %s\n", privateKey)
	}
	b.WriteString("\n")

	for _, p := range peers {
		b.WriteString("[Peer]\n")
		fmt.Fprintf(&b, "PublicKey = %s\n", p.PublicKey)
		ep := p.Endpoint
		if node.PeerEndpoints != nil {
			if override, ok := node.PeerEndpoints[p.ID]; ok && override != "" {
				ep = override
			}
		}
		if ep != "" {
			fmt.Fprintf(&b, "Endpoint = %s\n", ep)
		}
		if len(p.AllowedIPs) > 0 {
			fmt.Fprintf(&b, "AllowedIPs = %s\n", strings.Join(p.AllowedIPs, ", "))
		}
		if p.Keepalive > 0 {
			fmt.Fprintf(&b, "PersistentKeepalive = %d\n", p.Keepalive)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}
