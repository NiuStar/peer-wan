package model

import "time"

// HealthReport captures periodic health metrics for a node.
type HealthReport struct {
	NodeID     string             `json:"nodeId"`
	Status     string             `json:"status"` // up/degraded/down
	LatencyMs  map[string]int     `json:"latencyMs,omitempty"`
	PacketLoss map[string]float64 `json:"packetLoss,omitempty"`
	FRRState   map[string]string  `json:"frrState,omitempty"` // neighbor -> state
	Timestamp  time.Time          `json:"timestamp"`
}

// HealthSample is a thin wrapper used for history responses.
type HealthSample struct {
	Timestamp  time.Time          `json:"timestamp"`
	LatencyMs  map[string]int     `json:"latencyMs,omitempty"`
	PacketLoss map[string]float64 `json:"packetLoss,omitempty"`
}
