package model

// VersionedConfig captures config version metadata returned to agents.
type VersionedConfig struct {
	ConfigVersion string `json:"configVersion"`
	Version       int    `json:"version"`
}
