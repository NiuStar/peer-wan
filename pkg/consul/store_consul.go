//go:build consul

package consul

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	consulapi "github.com/hashicorp/consul/api"

	"peer-wan/pkg/model"
)

// Store is a Consul-backed NodeStore implementation.
type Store struct {
	cli     *consulapi.Client
	session *consulapi.Session
}

const (
	nodePrefix   = "peer-wan/nodes/"
	healthPrefix = "peer-wan/health/"
	auditPrefix  = "peer-wan/audit/"
	planPrefix   = "peer-wan/plan/"
	versionKey   = "peer-wan/plan/version"
)

func NewStore(addr string) *Store {
	cfg := consulapi.DefaultConfig()
	if addr != "" {
		cfg.Address = addr
	}
	cli, _ := consulapi.NewClient(cfg) // ignore error for build; runtime will report
	return &Store{cli: cli, session: cli.Session()}
}

func (s *Store) UpsertNode(n model.Node) (model.Node, error) {
	if s.cli == nil {
		return n, fmt.Errorf("consul client not configured")
	}
	b, err := json.Marshal(n)
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
		var n model.Node
		if err := json.Unmarshal(p.Value, &n); err == nil {
			out = append(out, n)
		}
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
	var n model.Node
	if err := json.Unmarshal(kv.Value, &n); err != nil {
		return model.Node{}, false, err
	}
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
	_, err = s.cli.KV().Put(&consulapi.KVPair{Key: healthPrefix + h.NodeID, Value: b}, nil)
	return err
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
