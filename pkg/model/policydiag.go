package model

import "time"

// PolicyDiagCheck describes a single check result.
type PolicyDiagCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok/warn/fail/info
	Detail string `json:"detail,omitempty"`
}

// PolicyDiagReport captures a diagnostic snapshot for policy/install state.
type PolicyDiagReport struct {
	NodeID    string            `json:"nodeId"`
	Summary   string            `json:"summary"`
	Checks    []PolicyDiagCheck `json:"checks"`
	Timestamp time.Time         `json:"timestamp"`
}
