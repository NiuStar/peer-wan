package store

import "peer-wan/pkg/model"

// NodeStore defines the persistence layer for node state.
// Later this can be backed by Consul KV, but we start with an in-memory impl.
type NodeStore interface {
	UpsertNode(model.Node) (model.Node, error)
	ListNodes() ([]model.Node, error)
	GetNode(id string) (model.Node, bool, error)
	SavePlan(model.Plan) error
	GetPlan(nodeID string) (model.Plan, bool, error)
	ListPlanHistory(nodeID string, limit int) ([]model.Plan, error)
	RollbackPlan(nodeID string, version int64) (model.Plan, error)
	SetGlobalPlanVersion(int64) error
	GetGlobalPlanVersion() (int64, error)
	UpdatePolicy(nodeID string, egressPeer string, rules []model.PolicyRule) error
	SaveHealth(model.HealthReport) error
	ListHealth() ([]model.HealthReport, error)
	AppendAudit(model.AuditEntry) error
	ListAudit(limit int) ([]model.AuditEntry, error)
}

// NewMemory is a helper to construct the in-memory implementation without importing it directly.
func NewMemory() NodeStore {
	return NewMemoryStore()
}
