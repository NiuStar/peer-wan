//go:build consul

package consul

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	consulapi "github.com/hashicorp/consul/api"

	"peer-wan/pkg/model"
)

// Store is a Consul-backed NodeStore implementation.
type Store struct {
	cli     *consulapi.Client
	session *consulapi.Session
}

type nodeRecord struct {
	model.Node
	PrivateKey     string `json:"privateKey,omitempty"`
	ProvisionToken string `json:"provisionToken,omitempty"`
}

const (
	nodePrefix       = "peer-wan/nodes/"
	healthPrefix     = "peer-wan/health/"
	healthHistPrefix = "peer-wan/health-history/"
	policyStatusPref = "peer-wan/policy-status/"
	policyDiagPref   = "peer-wan/policy-diag/"
	taskPref         = "peer-wan/tasks/"
	auditPrefix      = "peer-wan/audit/"
	planPrefix       = "peer-wan/plan/"
	versionKey       = "peer-wan/plan/version"
	settingsKey      = "peer-wan/settings"
)

func NewStore(addr string) *Store {
	cfg := consulapi.DefaultConfig()
	if addr != "" {
		cfg.Address = addr
	}
	cli, _ := consulapi.NewClient(cfg) // ignore error for build; runtime will report
	return &Store{cli: cli, session: cli.Session()}
}

// Ping checks consul leader status to ensure connectivity.
func (s *Store) Ping() error {
	if s.cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	_, err := s.cli.Status().Leader()
	return err
}

func (s *Store) UpsertNode(n model.Node) (model.Node, error) {
	if s.cli == nil {
		return n, fmt.Errorf("consul client not configured")
	}
	rec := nodeRecord{Node: n, PrivateKey: n.PrivateKey, ProvisionToken: n.ProvisionToken}
	b, err := json.Marshal(rec)
	if err != nil {
		return n, err
	}
	_, err = s.cli.KV().Put(&consulapi.KVPair{Key: nodePrefix + n.ID, Value: b}, nil)
	if err != nil {
		return n, err
	}
	return n, nil
}

func (s *Store) ListNodes() ([]model.Node, error) {
	if s.cli == nil {
		return nil, fmt.Errorf("consul client not configured")
	}
	pairs, _, err := s.cli.KV().List(nodePrefix, nil)
	if err != nil {
		return nil, err
	}
	var out []model.Node
	for _, p := range pairs {
		rec, err := decodeNodeRecord(p.Value)
		if err != nil {
			continue
		}
		n := rec.Node
		n.PrivateKey = rec.PrivateKey
		n.ProvisionToken = rec.ProvisionToken
		out = append(out, n)
	}
	return out, nil
}

func (s *Store) GetNode(id string) (model.Node, bool, error) {
	if s.cli == nil {
		return model.Node{}, false, fmt.Errorf("consul client not configured")
	}
	kv, _, err := s.cli.KV().Get(nodePrefix+id, nil)
	if err != nil || kv == nil {
		return model.Node{}, false, err
	}
	rec, err := decodeNodeRecord(kv.Value)
	if err != nil {
		return model.Node{}, false, err
	}
	n := rec.Node
	n.PrivateKey = rec.PrivateKey
	n.ProvisionToken = rec.ProvisionToken
	return n, true, nil
}

func (s *Store) SaveHealth(h model.HealthReport) error {
	if s.cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if _, err = s.cli.KV().Put(&consulapi.KVPair{Key: healthPrefix + h.NodeID, Value: b}, nil); err != nil {
		return err
	}
	// history entry keyed by timestamp
	histKey := fmt.Sprintf("%s%s/%d", healthHistPrefix, h.NodeID, h.Timestamp.UnixNano())
	_, _ = s.cli.KV().Put(&consulapi.KVPair{Key: histKey, Value: b}, nil)
	// best-effort prune >24h
	go s.pruneHealthHistory(h.NodeID, time.Now().Add(-24*time.Hour))
	return nil
}

func (s *Store) ListHealth() ([]model.HealthReport, error) {
	if s.cli == nil {
		return nil, fmt.Errorf("consul client not configured")
	}
	pairs, _, err := s.cli.KV().List(healthPrefix, nil)
	if err != nil {
		return nil, err
	}
	var out []model.HealthReport
	for _, p := range pairs {
		var h model.HealthReport
		if err := json.Unmarshal(p.Value, &h); err == nil {
			out = append(out, h)
		}
	}
	return out, nil
}

func (s *Store) ListHealthHistory(nodeID string, since time.Time) ([]model.HealthReport, error) {
	if s.cli == nil {
		return nil, fmt.Errorf("consul client not configured")
	}
	prefix := healthHistPrefix + nodeID + "/"
	pairs, _, err := s.cli.KV().List(prefix, nil)
	if err != nil {
		return nil, err
	}
	var out []model.HealthReport
	for _, p := range pairs {
		var h model.HealthReport
		if err := json.Unmarshal(p.Value, &h); err != nil {
			continue
		}
		if h.Timestamp.After(since) || h.Timestamp.Equal(since) {
			out = append(out, h)
		}
	}
	return out, nil
}

// pruneHealthHistory deletes history entries older than cutoff.
func (s *Store) pruneHealthHistory(nodeID string, cutoff time.Time) {
	if s.cli == nil {
		return
	}
	prefix := healthHistPrefix + nodeID + "/"
	pairs, _, err := s.cli.KV().List(prefix, nil)
	if err != nil {
		return
	}
	for _, p := range pairs {
		parts := strings.Split(p.Key, "/")
		if len(parts) == 0 {
			continue
		}
		tsStr := parts[len(parts)-1]
		if ts, errParse := strconv.ParseInt(tsStr, 10, 64); errParse == nil {
			if time.Unix(0, ts).Before(cutoff) {
				_, _ = s.cli.KV().Delete(p.Key, nil)
			}
		}
	}
}

// PruneHealthBefore removes health history older than cutoff for all nodes.
func (s *Store) PruneHealthBefore(cutoff time.Time) error {
	if s.cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	pairs, _, err := s.cli.KV().List(healthHistPrefix, nil)
	if err != nil {
		return err
	}
	for _, p := range pairs {
		parts := strings.Split(p.Key, "/")
		if len(parts) == 0 {
			continue
		}
		tsStr := parts[len(parts)-1]
		if ts, errParse := strconv.ParseInt(tsStr, 10, 64); errParse == nil {
			if time.Unix(0, ts).Before(cutoff) {
				_, _ = s.cli.KV().Delete(p.Key, nil)
			}
		}
	}
	return nil
}

func (s *Store) SavePolicyStatus(l model.PolicyInstallLog) error {
	if s.cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	if l.Timestamp.IsZero() {
		l.Timestamp = time.Now()
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s%s/%d", policyStatusPref, l.NodeID, l.Timestamp.UnixNano())
	_, err = s.cli.KV().Put(&consulapi.KVPair{Key: key, Value: b}, nil)
	return err
}

func (s *Store) ListPolicyStatus(nodeID string, limit int) ([]model.PolicyInstallLog, error) {
	if s.cli == nil {
		return nil, fmt.Errorf("consul client not configured")
	}
	pairs, _, err := s.cli.KV().List(policyStatusPref+nodeID+"/", nil)
	if err != nil {
		return nil, err
	}
	var out []model.PolicyInstallLog
	for _, p := range pairs {
		var l model.PolicyInstallLog
		if err := json.Unmarshal(p.Value, &l); err == nil {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (s *Store) SavePolicyDiag(d model.PolicyDiagReport) error {
	if s.cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	if d.Timestamp.IsZero() {
		d.Timestamp = time.Now()
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s%s/%d", policyDiagPref, d.NodeID, d.Timestamp.UnixNano())
	_, err = s.cli.KV().Put(&consulapi.KVPair{Key: key, Value: b}, nil)
	return err
}

func (s *Store) ListPolicyDiag(nodeID string, limit int) ([]model.PolicyDiagReport, error) {
	if s.cli == nil {
		return nil, fmt.Errorf("consul client not configured")
	}
	pairs, _, err := s.cli.KV().List(policyDiagPref+nodeID+"/", nil)
	if err != nil {
		return nil, err
	}
	var out []model.PolicyDiagReport
	for _, p := range pairs {
		var d model.PolicyDiagReport
		if err := json.Unmarshal(p.Value, &d); err == nil {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (s *Store) SaveTask(t model.Task) error {
	if s.cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	t.UpdatedAt = time.Now()
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s%s", taskPref, t.ID)
	_, err = s.cli.KV().Put(&consulapi.KVPair{Key: key, Value: b}, nil)
	return err
}

func (s *Store) GetTask(id string) (model.Task, bool, error) {
	if s.cli == nil {
		return model.Task{}, false, fmt.Errorf("consul client not configured")
	}
	kv, _, err := s.cli.KV().Get(taskPref+id, nil)
	if err != nil || kv == nil {
		return model.Task{}, false, err
	}
	var t model.Task
	if err := json.Unmarshal(kv.Value, &t); err != nil {
		return model.Task{}, false, err
	}
	return t, true, nil
}

func (s *Store) ListTasks(nodeID string, limit int) ([]model.Task, error) {
	if s.cli == nil {
		return nil, fmt.Errorf("consul client not configured")
	}
	pairs, _, err := s.cli.KV().List(taskPref, nil)
	if err != nil {
		return nil, err
	}
	var out []model.Task
	for _, p := range pairs {
		var t model.Task
		if err := json.Unmarshal(p.Value, &t); err == nil {
			if nodeID == "" || t.NodeID == nodeID {
				out = append(out, t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (s *Store) AppendAudit(entry model.AuditEntry) error {
	if s.cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s%d-%s", auditPrefix, entry.Timestamp.UnixNano(), entry.Target)
	_, err = s.cli.KV().Put(&consulapi.KVPair{Key: key, Value: b}, nil)
	return err
}

func (s *Store) SavePlan(p model.Plan) error {
	if s.cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	// CAS on latest plan by ModifyIndex (best effort).
	kv := &consulapi.KVPair{Key: planPrefix + p.NodeID, Value: b}
	if p.Version > 0 {
		kv.ModifyIndex = uint64(p.Version)
	}
	ok, _, err := s.cli.KV().CAS(kv, nil)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("plan CAS failed")
	}
	histKey := fmt.Sprintf("%s%s/%d", planPrefix, p.NodeID, p.Version)
	_, err = s.cli.KV().Put(&consulapi.KVPair{Key: histKey, Value: b}, nil)
	return err
}

func (s *Store) GetPlan(nodeID string) (model.Plan, bool, error) {
	if s.cli == nil {
		return model.Plan{}, false, fmt.Errorf("consul client not configured")
	}
	kv, _, err := s.cli.KV().Get(planPrefix+nodeID, nil)
	if err != nil || kv == nil {
		return model.Plan{}, false, err
	}
	var p model.Plan
	if err := json.Unmarshal(kv.Value, &p); err != nil {
		return model.Plan{}, false, err
	}
	return p, true, nil
}

func (s *Store) ListPlanHistory(nodeID string, limit int) ([]model.Plan, error) {
	if s.cli == nil {
		return nil, fmt.Errorf("consul client not configured")
	}
	prefix := fmt.Sprintf("%s%s/", planPrefix, nodeID)
	pairs, _, err := s.cli.KV().List(prefix, nil)
	if err != nil {
		return nil, err
	}
	var out []model.Plan
	for _, p := range pairs {
		var plan model.Plan
		if err := json.Unmarshal(p.Value, &plan); err == nil {
			out = append(out, plan)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (s *Store) RollbackPlan(nodeID string, version int64) (model.Plan, error) {
	if s.cli == nil {
		return model.Plan{}, fmt.Errorf("consul client not configured")
	}
	histKey := fmt.Sprintf("%s%s/%d", planPrefix, nodeID, version)
	kv, _, err := s.cli.KV().Get(histKey, nil)
	if err != nil || kv == nil {
		return model.Plan{}, fmt.Errorf("plan version not found")
	}
	var p model.Plan
	if err := json.Unmarshal(kv.Value, &p); err != nil {
		return model.Plan{}, err
	}
	// write back as latest
	p.Version = version
	if err := s.SavePlan(p); err != nil {
		return model.Plan{}, err
	}
	return p, nil
}

func (s *Store) SetGlobalPlanVersion(v int64) error {
	if s.cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	b := []byte(fmt.Sprintf("%d", v))
	_, err := s.cli.KV().Put(&consulapi.KVPair{Key: versionKey, Value: b}, nil)
	return err
}

func (s *Store) GetGlobalPlanVersion() (int64, error) {
	if s.cli == nil {
		return 0, fmt.Errorf("consul client not configured")
	}
	kv, _, err := s.cli.KV().Get(versionKey, nil)
	if err != nil || kv == nil {
		return 0, err
	}
	var v int64
	_, _ = fmt.Sscanf(string(kv.Value), "%d", &v)
	return v, nil
}

func (s *Store) UpdatePolicy(nodeID string, egressPeer string, rules []model.PolicyRule, defaultRoute bool, bypass []string, defaultRouteNextHop string) error {
	if s.cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	nodeKV, _, err := s.cli.KV().Get(nodePrefix+nodeID, nil)
	if err != nil {
		return err
	}
	if nodeKV == nil {
		return fmt.Errorf("node not found")
	}
	rec, err := decodeNodeRecord(nodeKV.Value)
	if err != nil {
		return err
	}
	rec.Node.EgressPeerID = egressPeer
	rec.Node.PolicyRules = rules
	rec.Node.DefaultRoute = defaultRoute
	rec.Node.BypassCIDRs = bypass
	rec.Node.DefaultRouteNextHop = defaultRouteNextHop
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = s.cli.KV().Put(&consulapi.KVPair{Key: nodePrefix + nodeID, Value: b}, nil)
	return err
}

func (s *Store) ListAudit(limit int) ([]model.AuditEntry, error) {
	if s.cli == nil {
		return nil, fmt.Errorf("consul client not configured")
	}
	pairs, _, err := s.cli.KV().List(auditPrefix, nil)
	if err != nil {
		return nil, err
	}
	var out []model.AuditEntry
	for _, p := range pairs {
		var e model.AuditEntry
		if err := json.Unmarshal(p.Value, &e); err == nil {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// Client exposes the underlying Consul client for watch helpers.
func (s *Store) Client() *consulapi.Client {
	return s.cli
}

// WatchPrefix returns a blocking query channel for changes on a prefix.
func WatchPrefix(ctx context.Context, cli *consulapi.Client, prefix string, out chan<- []*consulapi.KVPair) error {
	if cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	q := &consulapi.QueryOptions{}
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			kv, meta, err := cli.KV().List(prefix, q)
			if err == nil && kv != nil {
				out <- kv
				q.WaitIndex = meta.LastIndex
			} else {
				time.Sleep(time.Second)
			}
		}
	}()
	return nil
}

// decodeNodeRecord handles legacy numeric version fields by coercing to string.
func decodeNodeRecord(b []byte) (nodeRecord, error) {
	var rec nodeRecord
	if err := json.Unmarshal(b, &rec); err == nil {
		if rec.Node.ConfigVersion == "" && rec.Node.Version != "" {
			rec.Node.ConfigVersion = rec.Node.Version
		}
		return rec, nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return rec, err
	}
	if v, ok := raw["version"]; ok {
		switch t := v.(type) {
		case float64:
			raw["version"] = fmt.Sprintf("v0.0.%d", int(t))
		case int64:
			raw["version"] = fmt.Sprintf("v0.0.%d", t)
		case string:
			raw["version"] = t
		}
	}
	if _, ok := raw["configVersion"]; !ok && raw["version"] != nil {
		raw["configVersion"] = raw["version"]
	}
	b2, err := json.Marshal(raw)
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(b2, &rec); err != nil {
		return rec, err
	}
	return rec, nil
}

func (s *Store) GetSettings() (model.Settings, error) {
	if s.cli == nil {
		return model.Settings{}, fmt.Errorf("consul client not configured")
	}
	kv, _, err := s.cli.KV().Get(settingsKey, nil)
	if err != nil || kv == nil {
		return model.Settings{}, err
	}
	var cfg model.Settings
	if err := json.Unmarshal(kv.Value, &cfg); err != nil {
		return model.Settings{}, err
	}
	return cfg, nil
}

func (s *Store) UpdateSettings(cfg model.Settings) error {
	if s.cli == nil {
		return fmt.Errorf("consul client not configured")
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.cli.KV().Put(&consulapi.KVPair{Key: settingsKey, Value: b}, nil)
	return err
}
