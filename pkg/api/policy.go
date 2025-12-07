package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"peer-wan/pkg/model"
	"peer-wan/pkg/store"
)

type PolicyRequest struct {
	NodeID              string             `json:"nodeId"`
	EgressPeer          string             `json:"egressPeerId"`
	PolicyRules         []model.PolicyRule `json:"policyRules"`
	DefaultRoute        bool               `json:"defaultRoute,omitempty"`
	BypassCIDRs         []string           `json:"bypassCidrs,omitempty"`
	DefaultRouteNextHop string             `json:"defaultRouteNextHop,omitempty"`
}

func RegisterPolicyRoutes(mux *http.ServeMux, store store.NodeStore, auth func(r *http.Request) bool, planVersion *int64) {
	mux.HandleFunc("/api/v1/policy", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPost:
			var req PolicyRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NodeID == "" {
				http.Error(w, "invalid payload", http.StatusBadRequest)
				return
			}
			if len(req.PolicyRules) > 0 {
				valid := 0
				for i := range req.PolicyRules {
					// allow missing mask -> /32 auto-filled later
					req.PolicyRules[i].Prefix = strings.TrimSpace(req.PolicyRules[i].Prefix)
					// ensure ViaNode mirrors path tail for compatibility
					if req.PolicyRules[i].ViaNode == "" && len(req.PolicyRules[i].Path) > 0 {
						req.PolicyRules[i].ViaNode = req.PolicyRules[i].Path[len(req.PolicyRules[i].Path)-1]
					}
					if req.PolicyRules[i].Validate() {
						valid++
					}
				}
				if valid == 0 {
					http.Error(w, "no valid policy rules", http.StatusBadRequest)
					return
				}
			}
			if err := store.UpdatePolicy(req.NodeID, req.EgressPeer, req.PolicyRules, req.DefaultRoute, req.BypassCIDRs, req.DefaultRouteNextHop); err != nil {
				http.Error(w, "failed to update policy", http.StatusInternalServerError)
				return
			}
			BumpPlanVersion(planVersion)
			if wsHubGlobal != nil {
				// create a task to push apply via WS
				id := uuid.NewString()
				t := model.Task{
					ID:        id,
					NodeID:    req.NodeID,
					Type:      "policy_apply",
					Status:    "running",
					Steps:     []model.TaskStep{{Name: "dispatch", Status: "running", Timestamp: time.Now(), Message: "sending to agent"}},
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				_ = store.SaveTask(t)
				wsHubGlobal.Send(req.NodeID, WSMessage{Type: "task", NodeID: req.NodeID, Payload: map[string]interface{}{"taskId": id, "type": "policy_apply"}})
			}
			// respond with current saved values
			if n, ok, _ := store.GetNode(req.NodeID); ok {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"status":              "ok",
					"egressPeerId":        n.EgressPeerID,
					"policyRules":         n.PolicyRules,
					"defaultRoute":        n.DefaultRoute,
					"bypassCidrs":         n.BypassCIDRs,
					"defaultRouteNextHop": n.DefaultRouteNextHop,
				})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok"})
		case http.MethodGet:
			nodeID := r.URL.Query().Get("nodeId")
			if nodeID == "" {
				http.Error(w, "nodeId is required", http.StatusBadRequest)
				return
			}
			n, ok, err := store.GetNode(nodeID)
			if err != nil || !ok {
				http.Error(w, "node not found", http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"egressPeerId":        n.EgressPeerID,
				"policyRules":         n.PolicyRules,
				"defaultRoute":        n.DefaultRoute,
				"bypassCidrs":         n.BypassCIDRs,
				"defaultRouteNextHop": n.DefaultRouteNextHop,
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

// RegisterPolicyCommandRoutes allows console to push commands to agent via WS hub.
func RegisterPolicyCommandRoutes(mux *http.ServeMux, auth func(r *http.Request) bool, hub *WSHub) {
	type cmdReq struct {
		NodeID string                 `json:"nodeId"`
		Action string                 `json:"action"`
		Data   map[string]interface{} `json:"data,omitempty"`
	}
	mux.HandleFunc("/api/v1/policy/command", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if hub == nil {
			http.Error(w, "ws hub not ready", http.StatusServiceUnavailable)
			return
		}
		var req cmdReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NodeID == "" || req.Action == "" {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		hub.Send(req.NodeID, WSMessage{Type: "command", NodeID: req.NodeID, Payload: req})
		writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
	})
}

// RegisterPolicyStatusRoutes handles policy install status logs from agents and frontend polling.
func RegisterPolicyStatusRoutes(mux *http.ServeMux, store store.NodeStore, auth func(r *http.Request) bool) {
	mux.HandleFunc("/api/v1/policy/status", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPost:
			var req model.PolicyInstallLog
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NodeID == "" || req.Status == "" {
				http.Error(w, "invalid payload", http.StatusBadRequest)
				return
			}
			if req.Timestamp.IsZero() {
				req.Timestamp = time.Now()
			}
			if err := store.SavePolicyStatus(req); err != nil {
				http.Error(w, "failed to save status", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case http.MethodGet:
			nodeID := r.URL.Query().Get("nodeId")
			if nodeID == "" {
				http.Error(w, "nodeId is required", http.StatusBadRequest)
				return
			}
			limit := 20
			if l := r.URL.Query().Get("limit"); l != "" {
				if n, err := strconv.Atoi(l); err == nil && n > 0 {
					limit = n
				}
			}
			items, err := store.ListPolicyStatus(nodeID, limit)
			if err != nil {
				http.Error(w, "failed to list status", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

// RegisterPolicyDiagRoutes serves policy install diagnostics reported by agent.
func RegisterPolicyDiagRoutes(mux *http.ServeMux, store store.NodeStore, auth func(r *http.Request) bool) {
	mux.HandleFunc("/api/v1/policy/diag", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPost:
			var req model.PolicyDiagReport
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NodeID == "" {
				http.Error(w, "invalid payload", http.StatusBadRequest)
				return
			}
			if req.Timestamp.IsZero() {
				req.Timestamp = time.Now()
			}
			if err := store.SavePolicyDiag(req); err != nil {
				http.Error(w, "failed to save diag", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case http.MethodGet:
			nodeID := r.URL.Query().Get("nodeId")
			if nodeID == "" {
				http.Error(w, "nodeId is required", http.StatusBadRequest)
				return
			}
			limit := 10
			if l := r.URL.Query().Get("limit"); l != "" {
				if n, err := strconv.Atoi(l); err == nil && n > 0 {
					limit = n
				}
			}
			items, err := store.ListPolicyDiag(nodeID, limit)
			if err != nil {
				http.Error(w, "failed to list diag", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
