package model

// Node captures registered node state and desired overlay properties.
type Node struct {
	ID             string            `json:"id"`
	PublicKey      string            `json:"publicKey"`
	Endpoints      []string          `json:"endpoints"`
	CIDRs          []string          `json:"cidrs"`
	ConfigVersion  string            `json:"configVersion"`
	Version        int               `json:"version"` // monotonically increasing config version
	ListenPort     int               `json:"listenPort,omitempty"`
	OverlayIP      string            `json:"overlayIp,omitempty"`
	ASN            int               `json:"asn,omitempty"`
	RouterID       string            `json:"routerId,omitempty"`
	EgressPeerID   string            `json:"egressPeerId,omitempty"`
	PolicyRules    []PolicyRule      `json:"policyRules,omitempty"`
	PrivateKey     string            `json:"-"`                       // stored only for bootstrap
	ProvisionToken string            `json:"-"`                       // one-time token
	PeerEndpoints  map[string]string `json:"peerEndpoints,omitempty"` // overrides target node endpoint per peer
}
