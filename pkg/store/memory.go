package store

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"peer-wan/pkg/model"
)

// MemoryStore is a simple in-memory implementation, intended for dev/demo.
type MemoryStore struct {
	mu                sync.RWMutex
	nodes             map[string]model.Node
	version           map[string]int
	health            map[string]model.HealthReport
	audit             []model.AuditEntry
	plans             map[string]model.Plan
	history           map[string][]model.Plan
	globalPlanVersion int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nodes:   make(map[string]model.Node),
		version: make(map[string]int),
		health:  make(map[string]model.HealthReport),
		plans:   make(map[string]model.Plan),
		history: make(map[string][]model.Plan),
	}
}

func (m *MemoryStore) UpsertNode(n model.Node) (model.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.version[n.ID]
	next := current + 1
	n.Version = next
	n.ConfigVersion = versionString(next)
	m.nodes[n.ID] = n
	m.version[n.ID] = next
	return n, nil
}

func (m *MemoryStore) ListNodes() ([]model.Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		out = append(out, n)
	}
	return out, nil
}

func (m *MemoryStore) GetNode(id string) (model.Node, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.nodes[id]
	return n, ok, nil
}

// LeaderGuard is a no-op leader hook for memory store; it simply runs cb once.
func (m *MemoryStore) LeaderGuard(ctx context.Context, _ string, _ time.Duration, cb func(context.Context)) {
	if cb != nil {
		cb(ctx)
	}
}

func versionString(v int) string {
	return "v0.0." + strconv.Itoa(v)
}

func (m *MemoryStore) SaveHealth(h model.HealthReport) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health[h.NodeID] = h
	return nil
}

func (m *MemoryStore) ListHealth() ([]model.HealthReport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.HealthReport, 0, len(m.health))
	for _, h := range m.health {
		out = append(out, h)
	}
	return out, nil
}

func (m *MemoryStore) AppendAudit(entry model.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audit = append(m.audit, entry)
	return nil
}

func (m *MemoryStore) ListAudit(limit int) ([]model.AuditEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > len(m.audit) {
		limit = len(m.audit)
	}
	out := make([]model.AuditEntry, 0, limit)
	start := len(m.audit) - limit
	for i := start; i < len(m.audit); i++ {
		out = append(out, m.audit[i])
	}
	return out, nil
}

func (m *MemoryStore) SavePlan(p model.Plan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[p.NodeID] = p
	h := append(m.history[p.NodeID], p)
	if len(h) > 20 {
		h = h[len(h)-20:]
	}
	m.history[p.NodeID] = h
	return nil
}

func (m *MemoryStore) GetPlan(nodeID string) (model.Plan, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.plans[nodeID]
	return p, ok, nil
}

func (m *MemoryStore) ListPlanHistory(nodeID string, limit int) ([]model.Plan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h := m.history[nodeID]
	if limit <= 0 || limit > len(h) {
		limit = len(h)
	}
	return append([]model.Plan(nil), h[len(h)-limit:]...), nil
}

func (m *MemoryStore) RollbackPlan(nodeID string, version int64) (model.Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.history[nodeID]
	for _, p := range h {
		if p.Version == version {
			m.plans[nodeID] = p
			m.globalPlanVersion = version
			return p, nil
		}
	}
	return model.Plan{}, fmt.Errorf("version not found")
}

func (m *MemoryStore) SetGlobalPlanVersion(v int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.globalPlanVersion = v
	return nil
}

func (m *MemoryStore) GetGlobalPlanVersion() (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.globalPlanVersion, nil
}
