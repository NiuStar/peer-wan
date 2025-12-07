package model

// PolicyRule defines a CIDR to be routed via a specific peer/node.
type PolicyRule struct {
	Prefix  string   `json:"prefix"`
	ViaNode string   `json:"viaNode"`           // peer/node ID to egress from (kept for backward compatibility)
	Path    []string `json:"path,omitempty"`    // ordered hop list; last element is egress
	Domains []string `json:"domains,omitempty"` // optional: domain list to resolve and add as host routes
}

// Validate returns true if the prefix and via node are present.
func (p PolicyRule) Validate() bool {
	hasTarget := p.ViaNode != "" || len(p.Path) > 0
	return (p.Prefix != "" || len(p.Domains) > 0) && hasTarget
}
