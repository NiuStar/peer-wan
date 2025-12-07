package model

import "time"

// PolicyInstallLog captures install/apply status reported by agent for policy/default-route changes.
type PolicyInstallLog struct {
	NodeID    string    `json:"nodeId"`
	Version   string    `json:"version,omitempty"`
	Status    string    `json:"status"` // applying/success/failed/checking
	Message   string    `json:"message,omitempty"`
	Logs      []string  `json:"logs,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
