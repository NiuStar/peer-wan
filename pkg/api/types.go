package api

import "peer-wan/pkg/model"

// NodeRegistrationRequest is sent by agents during bootstrap.
type NodeRegistrationRequest struct {
	ID         string   `json:"id"`
	PublicKey  string   `json:"publicKey"`
	Endpoints  []string `json:"endpoints"`
	CIDRs      []string `json:"cidrs"`
	ConfigHint string   `json:"configHint,omitempty"` // optional: version/intent hint
	Force      bool     `json:"force,omitempty"`      // force refresh even if unchanged
	ListenPort int      `json:"listenPort,omitempty"` // WireGuard listen port
	OverlayIP  string   `json:"overlayIp,omitempty"`  // WireGuard interface address (/32 recommended)
	ASN        int      `json:"asn,omitempty"`        // optional BGP ASN
	RouterID   string   `json:"routerId,omitempty"`   // optional BGP router-id (defaults to overlay IP)
}

// NodeConfigResponse carries the config the agent should apply.
type NodeConfigResponse struct {
	ID             string       `json:"id"`
	ConfigVersion  string       `json:"configVersion"`
	WireGuardPeers []model.Peer `json:"wireGuardPeers"`
	Routes         []string     `json:"routes"`
	Message        string       `json:"message,omitempty"`
}
