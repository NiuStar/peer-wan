package model

import "time"

// TaskStep captures a single step status for a node.
type TaskStep struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"` // pending/running/success/fail
	Message   string    `json:"message,omitempty"`
	NodeID    string    `json:"nodeId,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Task represents a multi-step action (policy apply/diagnose) across nodes.
type Task struct {
	ID            string     `json:"id"`
	NodeID        string     `json:"nodeId,omitempty"` // backward compat for single-node
	Targets       []string   `json:"targets,omitempty"`
	Type          string     `json:"type"` // policy_apply / policy_diag
	Status        string     `json:"status"`
	OverallStatus string     `json:"overallStatus,omitempty"`
	Steps         []TaskStep `json:"steps"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}
