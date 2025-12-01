package model

import "time"

// AuditEntry captures an operation against the control plane.
type AuditEntry struct {
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
