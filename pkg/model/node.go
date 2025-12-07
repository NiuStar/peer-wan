package model

// Node captures registered node state and desired overlay properties.
type Node struct {
	ID                  string            `json:"id"`
	PublicKey           string            `json:"publicKey"`
	Endpoints           []string          `json:"endpoints"`
	CIDRs               []string          `json:"cidrs"`
	ConfigVersion       string            `json:"configVersion"`
	Version             string            `json:"version"` // monotonically increasing config version (string)
	ListenPort          int               `json:"listenPort,omitempty"`
	OverlayIP           string            `json:"overlayIp,omitempty"`
	ASN                 int               `json:"asn,omitempty"`
	RouterID            string            `json:"routerId,omitempty"`
	EgressPeerID        string            `json:"egressPeerId,omitempty"`
	PolicyRules         []PolicyRule      `json:"policyRules,omitempty"`
	DefaultRoute        bool              `json:"defaultRoute,omitempty"`        // whether to install default via egress
	BypassCIDRs         []string          `json:"bypassCidrs,omitempty"`         // stay on local routing (management)
	DefaultRouteNextHop string            `json:"defaultRouteNextHop,omitempty"` // optional override for default next-hop node
	PrivateKey          string            `json:"-"`                             // stored only for bootstrap
	ProvisionToken      string            `json:"-"`                             // one-time token
	PeerEndpoints       map[string]string `json:"peerEndpoints,omitempty"`       // overrides target node endpoint per peer
}
