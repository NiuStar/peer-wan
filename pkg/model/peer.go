package model

// Peer describes a WireGuard peer and advertised networks.
type Peer struct {
	ID         string   `json:"id"`
	PublicKey  string   `json:"publicKey"`
	Endpoint   string   `json:"endpoint,omitempty"`
	AllowedIPs []string `json:"allowedIPs"`
	Keepalive  int      `json:"keepaliveSeconds,omitempty"`
}
