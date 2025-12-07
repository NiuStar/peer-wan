package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"peer-wan/pkg/model"
	"peer-wan/pkg/policy"
)

// MemoryStore is a simple in-memory implementation, intended for dev/demo.
type MemoryStore struct {
	mu                sync.RWMutex
	nodes             map[string]model.Node
	version           map[string]int
	health            map[string]model.HealthReport
	healthHistory     map[string][]model.HealthReport
	policyStatus      map[string][]model.PolicyInstallLog
	policyDiag        map[string][]model.PolicyDiagReport
	tasks             map[string]model.Task
	audit             []model.AuditEntry
	plans             map[string]model.Plan
	history           map[string][]model.Plan
	globalPlanVersion int64
	settings          model.Settings
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nodes:         make(map[string]model.Node),
		version:       make(map[string]int),
		health:        make(map[string]model.HealthReport),
		healthHistory: make(map[string][]model.HealthReport),
		policyStatus:  make(map[string][]model.PolicyInstallLog),
		policyDiag:    make(map[string][]model.PolicyDiagReport),
		tasks:         make(map[string]model.Task),
		plans:         make(map[string]model.Plan),
		history:       make(map[string][]model.Plan),
		settings: model.Settings{
			GeoIP: model.GeoIPConfig{
				CacheDir: policy.DefaultCacheDir(),
				SourceV4: policy.DefaultSourceV4(),
				SourceV6: policy.DefaultSourceV6(),
				CacheTTL: "24h",
			},
			Diag: model.DiagConfig{
				PingInterval: "3s",
			},
		},
	}
}

func (m *MemoryStore) UpsertNode(n model.Node) (model.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.version[n.ID]
	next := current + 1
	n.Version = versionString(next)
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

// Ping reports readiness for health/info endpoints.
func (m *MemoryStore) Ping() error { return nil }

func versionString(v int) string {
	return "v0.0." + strconv.Itoa(v)
}

func (m *MemoryStore) SaveHealth(h model.HealthReport) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health[h.NodeID] = h
	// append history and prune older than 24h
	cutoff := time.Now().Add(-24 * time.Hour)
	hist := append(m.healthHistory[h.NodeID], h)
	keep := hist[:0]
	for _, item := range hist {
		if item.Timestamp.After(cutoff) {
			keep = append(keep, item)
		}
	}
	m.healthHistory[h.NodeID] = keep
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

func (m *MemoryStore) ListHealthHistory(nodeID string, since time.Time) ([]model.HealthReport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hist := m.healthHistory[nodeID]
	out := make([]model.HealthReport, 0, len(hist))
	for _, h := range hist {
		if h.Timestamp.After(since) || h.Timestamp.Equal(since) {
			out = append(out, h)
		}
	}
	return out, nil
}

func (m *MemoryStore) PruneHealthBefore(cutoff time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for nodeID, hist := range m.healthHistory {
		keep := hist[:0]
		for _, h := range hist {
			if h.Timestamp.After(cutoff) || h.Timestamp.Equal(cutoff) {
				keep = append(keep, h)
			}
		}
		m.healthHistory[nodeID] = keep
	}
	return nil
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

func (m *MemoryStore) SavePolicyStatus(l model.PolicyInstallLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.Timestamp.IsZero() {
		l.Timestamp = time.Now()
	}
	list := append(m.policyStatus[l.NodeID], l)
	if len(list) > 50 {
		list = list[len(list)-50:]
	}
	m.policyStatus[l.NodeID] = list
	return nil
}

func (m *MemoryStore) ListPolicyStatus(nodeID string, limit int) ([]model.PolicyInstallLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.policyStatus[nodeID]
	if limit <= 0 || limit > len(list) {
		limit = len(list)
	}
	out := make([]model.PolicyInstallLog, 0, limit)
	start := len(list) - limit
	for i := start; i < len(list); i++ {
		out = append(out, list[i])
	}
	return out, nil
}

func (m *MemoryStore) SavePolicyDiag(d model.PolicyDiagReport) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.Timestamp.IsZero() {
		d.Timestamp = time.Now()
	}
	list := append(m.policyDiag[d.NodeID], d)
	if len(list) > 20 {
		list = list[len(list)-20:]
	}
	m.policyDiag[d.NodeID] = list
	return nil
}

func (m *MemoryStore) ListPolicyDiag(nodeID string, limit int) ([]model.PolicyDiagReport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.policyDiag[nodeID]
	if limit <= 0 || limit > len(list) {
		limit = len(list)
	}
	start := len(list) - limit
	if start < 0 {
		start = 0
	}
	out := make([]model.PolicyDiagReport, 0, limit)
	for i := start; i < len(list); i++ {
		out = append(out, list[i])
	}
	return out, nil
}

func (m *MemoryStore) SaveTask(t model.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	t.UpdatedAt = time.Now()
	m.tasks[t.ID] = t
	return nil
}

func (m *MemoryStore) GetTask(id string) (model.Task, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	return t, ok, nil
}

func (m *MemoryStore) ListTasks(nodeID string, limit int) ([]model.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.Task{}
	for _, t := range m.tasks {
		if nodeID == "" || t.NodeID == nodeID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
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

func (m *MemoryStore) UpdatePolicy(nodeID string, egressPeer string, rules []model.PolicyRule, defaultRoute bool, bypass []string, defaultRouteNextHop string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found")
	}
	n.EgressPeerID = egressPeer
	n.PolicyRules = rules
	n.DefaultRoute = defaultRoute
	n.BypassCIDRs = bypass
	n.DefaultRouteNextHop = defaultRouteNextHop
	m.nodes[nodeID] = n
	return nil
}

func (m *MemoryStore) GetSettings() (model.Settings, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings, nil
}

func (m *MemoryStore) UpdateSettings(s model.Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings = s
	return nil
}
