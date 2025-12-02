package model

import "time"

// Plan represents a computed peer plan for a node, with versioning metadata.
type Plan struct {
	NodeID        string       `json:"nodeId"`
	Version       int64        `json:"version"`
	ConfigVersion string       `json:"configVersion"`
	Peers         []Peer       `json:"peers"`
	Routes        []string     `json:"routes"`
	CreatedAt     time.Time    `json:"createdAt"`
	Signature     string       `json:"signature,omitempty"`
	EgressPeerID  string       `json:"egressPeerId,omitempty"`
	PolicyRules   []PolicyRule `json:"policyRules,omitempty"`
}
