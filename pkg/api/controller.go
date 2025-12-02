package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"peer-wan/pkg/model"
	"peer-wan/pkg/store"
	"peer-wan/pkg/topology"
)

// RegisterRoutes wires the HTTP handlers on the provided mux.
func RegisterRoutes(mux *http.ServeMux, store store.NodeStore, token string, planVersion *int64, controllerAddr, storeType, consulAddr string) {
	authHandler := &AuthHandler{DB: dbRef}
	authHandler.RegisterRoutes(mux)
	auth := authFuncJWT
	RegisterPolicyRoutes(mux, store, auth, planVersion)
	RegisterPrepareRoute(mux, store, planVersion, auth, controllerAddr)

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("peer-wan controller"))
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/api/v1/info", func(w http.ResponseWriter, _ *http.Request) {
		info := map[string]interface{}{
			"store":       storeType,
			"consulAddr":  consulAddr,
			"publicAddr":  controllerAddr,
			"planVersion": atomic.LoadInt64(planVersion),
		}
		if dbRef != nil {
			sqlDB, err := dbRef.DB()
			if err == nil {
				if errPing := sqlDB.Ping(); errPing == nil {
					info["mysql"] = "ok"
				} else {
					info["mysql"] = errPing.Error()
				}
			} else {
				info["mysql"] = err.Error()
			}
		}
		if pinger, ok := store.(interface{ Ping() error }); ok {
			if err := pinger.Ping(); err != nil {
				info["storeStatus"] = err.Error()
			} else {
				info["storeStatus"] = "ok"
			}
		}
		writeJSON(w, http.StatusOK, info)
	})

	mux.HandleFunc("/api/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		nodes, err := store.ListNodes()
		if err != nil {
			http.Error(w, "failed to list nodes", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, nodes)
	})

	mux.HandleFunc("/api/v1/nodes/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req NodeRegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		if req.ProvisionToken == "" {
			req.ProvisionToken = r.Header.Get("X-Provision-Token")
		}
		if req.ID == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}

		allowWithoutJWT := req.ProvisionToken != ""
		if !allowWithoutJWT && !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		existing, ok, _ := store.GetNode(req.ID)
		if allowWithoutJWT {
			// provisioning path: validate one-time token and merge controller-assigned fields
			if !ok || existing.ProvisionToken == "" || existing.ProvisionToken != req.ProvisionToken {
				http.Error(w, "invalid provision token", http.StatusUnauthorized)
				return
			}
		}

		node := model.Node{
			ID:         req.ID,
			PublicKey:  req.PublicKey,
			Endpoints:  req.Endpoints,
			CIDRs:      req.CIDRs,
			ListenPort: req.ListenPort,
			OverlayIP:  req.OverlayIP,
			ASN:        req.ASN,
			RouterID:   req.RouterID,
		}

		if allowWithoutJWT {
			// populate from prepared record; agent can override by sending non-empty fields.
			if node.PublicKey == "" {
				node.PublicKey = existing.PublicKey
			}
			if node.PrivateKey == "" {
				node.PrivateKey = existing.PrivateKey
			}
			if node.OverlayIP == "" {
				node.OverlayIP = existing.OverlayIP
			}
			node.ProvisionToken = existing.ProvisionToken
			if node.ListenPort == 0 {
				node.ListenPort = existing.ListenPort
			}
			if node.ASN == 0 {
				node.ASN = existing.ASN
			}
			if node.RouterID == "" {
				node.RouterID = existing.RouterID
			}
			if len(node.Endpoints) == 0 {
				node.Endpoints = existing.Endpoints
			}
			if len(node.CIDRs) == 0 {
				node.CIDRs = existing.CIDRs
			}
		} else if ok {
			// UI/API 编辑路径：合并已有字段，保留未提交的值
			if node.PublicKey == "" {
				node.PublicKey = existing.PublicKey
			}
			if node.PrivateKey == "" {
				node.PrivateKey = existing.PrivateKey
			}
			if node.OverlayIP == "" {
				node.OverlayIP = existing.OverlayIP
			}
			if node.ListenPort == 0 {
				node.ListenPort = existing.ListenPort
			}
			if node.ASN == 0 {
				node.ASN = existing.ASN
			}
			if node.RouterID == "" {
				node.RouterID = existing.RouterID
			}
			if len(node.Endpoints) == 0 {
				node.Endpoints = existing.Endpoints
			}
			if len(node.CIDRs) == 0 {
				node.CIDRs = existing.CIDRs
			}
		}
		if node.RouterID == "" && node.OverlayIP != "" {
			if idx := strings.Index(node.OverlayIP, "/"); idx > 0 {
				node.RouterID = node.OverlayIP[:idx]
			}
		}

		var saved model.Node
		if ok && !req.Force && nodeEqual(existing, node) {
			saved = existing
		} else {
			var err error
			saved, err = store.UpsertNode(node)
			if err != nil {
				http.Error(w, "failed to persist node", http.StatusInternalServerError)
				return
			}
		}
		_ = store.AppendAudit(model.AuditEntry{
			Actor:     "controller",
			Action:    "register",
			Target:    saved.ID,
			Detail:    "node registered/updated",
			Timestamp: time.Now(),
		})

		allNodes, err := store.ListNodes()
		if err != nil {
			http.Error(w, "failed to list nodes", http.StatusInternalServerError)
			return
		}
		healthList, _ := store.ListHealth()
		hmap := make(map[string]model.HealthReport)
		for _, h := range healthList {
			hmap[h.NodeID] = h
		}
		peerPlan := topology.BuildPeerPlan(saved.ID, allNodes, hmap)
		savePlan(store, saved, peerPlan, planVersion)
		BumpPlanVersion(planVersion)
		log.Printf("registered node %s endpoints=%v cidrs=%v version=%s", saved.ID, saved.Endpoints, saved.CIDRs, saved.ConfigVersion)

		resp := NodeConfigResponse{
			ID:             saved.ID,
			ConfigVersion:  saved.ConfigVersion,
			WireGuardPeers: peerPlan,
			Routes:         saved.CIDRs,
			OverlayIP:      saved.OverlayIP,
			ListenPort:     saved.ListenPort,
			ASN:            saved.ASN,
			RouterID:       saved.RouterID,
			Endpoints:      saved.Endpoints,
			PrivateKey:     saved.PrivateKey,
			PublicKey:      saved.PublicKey,
			EgressPeerID:   saved.EgressPeerID,
			PolicyRules:    saved.PolicyRules,
			Message:        "registered; peer plan derived from currently known nodes",
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var report model.HealthReport
			if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
				http.Error(w, "invalid payload", http.StatusBadRequest)
				return
			}
			report.Timestamp = time.Now()
			if report.NodeID == "" {
				http.Error(w, "nodeId is required", http.StatusBadRequest)
				return
			}
			if !auth(r) && !agentAuthorized(store, report.NodeID, r.Header.Get("X-Provision-Token")) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if err := store.SaveHealth(report); err != nil {
				http.Error(w, "failed to save health", http.StatusInternalServerError)
				return
			}
			// recalc plan for this node and store
			nodes, _ := store.ListNodes()
			hmap := map[string]model.HealthReport{report.NodeID: report}
			peerPlan := topology.BuildPeerPlan(report.NodeID, nodes, hmap)
			savePlan(store, model.Node{ID: report.NodeID}, peerPlan, planVersion)
			BumpPlanVersion(planVersion)
			_ = store.AppendAudit(model.AuditEntry{
				Actor:     report.NodeID,
				Action:    "health_report",
				Target:    "self",
				Timestamp: report.Timestamp,
			})
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if r.Method == http.MethodGet {
			health, err := store.ListHealth()
			if err != nil {
				http.Error(w, "failed to list health", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, health)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/api/v1/audit", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		entries, err := store.ListAudit(50)
		if err != nil {
			http.Error(w, "failed to list audit", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, entries)
	})

	mux.HandleFunc("/api/v1/plan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		nodeID := r.URL.Query().Get("nodeId")
		if nodeID == "" {
			http.Error(w, "nodeId is required", http.StatusBadRequest)
			return
		}
		if !auth(r) && !agentAuthorized(store, nodeID, r.Header.Get("X-Provision-Token")) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		waitStr := r.URL.Query().Get("waitVersion")
		if waitStr != "" && planVersion != nil {
			waitForVersion(planVersion, waitStr)
		}
		nodes, err := store.ListNodes()
		if err != nil {
			http.Error(w, "failed to list nodes", http.StatusInternalServerError)
			return
		}
		healthList, _ := store.ListHealth()
		hmap := make(map[string]model.HealthReport)
		for _, h := range healthList {
			hmap[h.NodeID] = h
		}
		peerPlan := topology.BuildPeerPlan(nodeID, nodes, hmap)
		var target model.Node
		for _, n := range nodes {
			if n.ID == nodeID {
				target = n
				break
			}
		}
		version := "dynamic-" + time.Now().Format(time.RFC3339Nano)
		if planVersion != nil {
			v := atomic.LoadInt64(planVersion)
			if v > 0 {
				version = "dynamic-v" + itoa(v)
			}
		}
		savePlan(store, target, peerPlan, planVersion)
		resp := NodeConfigResponse{
			ID:             nodeID,
			ConfigVersion:  version,
			WireGuardPeers: peerPlan,
			Routes:         target.CIDRs,
			OverlayIP:      target.OverlayIP,
			ListenPort:     target.ListenPort,
			ASN:            target.ASN,
			RouterID:       target.RouterID,
			Endpoints:      target.Endpoints,
			EgressPeerID:   target.EgressPeerID,
			PolicyRules:    target.PolicyRules,
			Message:        "dynamic plan based on current health",
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("/api/v1/plan/history", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		nodeID := r.URL.Query().Get("nodeId")
		if nodeID == "" {
			http.Error(w, "nodeId is required", http.StatusBadRequest)
			return
		}
		plans, err := store.ListPlanHistory(nodeID, 20)
		if err != nil {
			http.Error(w, "failed to list plan history", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, plans)
	})

	mux.HandleFunc("/api/v1/plan/rollback", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			NodeID  string `json:"nodeId"`
			Version int64  `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NodeID == "" {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		plan, err := store.RollbackPlan(req.NodeID, req.Version)
		if err != nil {
			http.Error(w, "rollback failed", http.StatusInternalServerError)
			return
		}
		if plan.Signature != "" && plan.Signature != signPlan(model.Node{ID: plan.NodeID, CIDRs: plan.Routes}, plan.Peers, plan.ConfigVersion) {
			http.Error(w, "rollback signature mismatch", http.StatusBadRequest)
			return
		}
		atomic.StoreInt64(planVersion, plan.Version)
		_ = store.SetGlobalPlanVersion(plan.Version)
		_ = store.AppendAudit(model.AuditEntry{
			Actor:     "controller",
			Action:    "rollback_plan",
			Target:    plan.NodeID,
			Detail:    "rollback to version " + itoa(plan.Version),
			Timestamp: time.Now(),
		})
		writeJSON(w, http.StatusOK, plan)
	})
}

func savePlan(store store.NodeStore, node model.Node, peers []model.Peer, planVersion *int64) {
	var version int64
	if planVersion != nil {
		version = atomic.AddInt64(planVersion, 1)
	}
	cv := "dynamic-" + time.Now().Format(time.RFC3339Nano)
	if version > 0 {
		cv = "dynamic-v" + itoa(version)
	}
	p := model.Plan{
		NodeID:        node.ID,
		Version:       version,
		ConfigVersion: cv,
		Peers:         peers,
		Routes:        node.CIDRs,
		CreatedAt:     time.Now(),
		Signature:     signPlan(node, peers, cv),
	}
	_ = store.SavePlan(p)
	_ = store.SetGlobalPlanVersion(version)
}

// RecomputeAllPlans recalculates peer plans for all nodes and stores them.
func RecomputeAllPlans(store store.NodeStore, planVersion *int64) error {
	nodes, err := store.ListNodes()
	if err != nil {
		return err
	}
	healthList, _ := store.ListHealth()
	hmap := make(map[string]model.HealthReport)
	for _, h := range healthList {
		hmap[h.NodeID] = h
	}
	for _, n := range nodes {
		peers := topology.BuildPeerPlan(n.ID, nodes, hmap)
		savePlan(store, n, peers, planVersion)
	}
	return nil
}

func signPlan(node model.Node, peers []model.Peer, cfgVer string) string {
	h := sha256.New()
	h.Write([]byte(node.ID))
	h.Write([]byte(cfgVer))
	for _, p := range peers {
		h.Write([]byte(p.ID))
		h.Write([]byte(strings.Join(p.AllowedIPs, ",")))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// BumpPlanVersion increments a shared plan version counter if provided.
func BumpPlanVersion(v *int64) {
	if v != nil {
		atomic.AddInt64(v, 1)
	}
}

// waitForVersion blocks up to 20s until planVersion exceeds waitVersion.
func waitForVersion(planVersion *int64, waitStr string) {
	target, _ := strconv.ParseInt(waitStr, 10, 64)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(planVersion) > target {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func nodeEqual(a, b model.Node) bool {
	if a.ID != b.ID || a.PublicKey != b.PublicKey || a.ListenPort != b.ListenPort || a.OverlayIP != b.OverlayIP {
		return false
	}
	if a.ASN != b.ASN || a.RouterID != b.RouterID {
		return false
	}
	if len(a.Endpoints) != len(b.Endpoints) || len(a.CIDRs) != len(b.CIDRs) {
		return false
	}
	for i := range a.Endpoints {
		if a.Endpoints[i] != b.Endpoints[i] {
			return false
		}
	}
	for i := range a.CIDRs {
		if a.CIDRs[i] != b.CIDRs[i] {
			return false
		}
	}
	return true
}

func agentAuthorized(store store.NodeStore, nodeID, token string) bool {
	if nodeID == "" || token == "" {
		return false
	}
	n, ok, err := store.GetNode(nodeID)
	if err != nil || !ok {
		return false
	}
	return n.ProvisionToken != "" && n.ProvisionToken == token
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func authFunc(token string) func(r *http.Request) bool {
	if token == "" {
		return func(_ *http.Request) bool { return true }
	}
	return func(r *http.Request) bool {
		h := r.Header.Get("X-Auth-Token")
		if h == "" {
			// also allow simple Bearer token
			authz := r.Header.Get("Authorization")
			if strings.HasPrefix(authz, "Bearer ") {
				h = strings.TrimPrefix(authz, "Bearer ")
			}
		}
		return h == token
	}
}
