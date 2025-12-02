package model

// PolicyRule defines a CIDR to be routed via a specific peer/node.
type PolicyRule struct {
	Prefix  string `json:"prefix"`
	ViaNode string `json:"viaNode"` // peer/node ID to egress from
}
