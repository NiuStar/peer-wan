package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"peer-wan/pkg/model"
	"peer-wan/pkg/store"
)

type PolicyRequest struct {
	NodeID      string             `json:"nodeId"`
	EgressPeer  string             `json:"egressPeerId"`
	PolicyRules []model.PolicyRule `json:"policyRules"`
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
					if req.PolicyRules[i].Validate() {
						valid++
					}
				}
				if valid == 0 {
					http.Error(w, "no valid policy rules", http.StatusBadRequest)
					return
				}
			}
			if err := store.UpdatePolicy(req.NodeID, req.EgressPeer, req.PolicyRules); err != nil {
				http.Error(w, "failed to update policy", http.StatusInternalServerError)
				return
			}
			BumpPlanVersion(planVersion)
			// respond with current saved values
			if n, ok, _ := store.GetNode(req.NodeID); ok {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"status":       "ok",
					"egressPeerId": n.EgressPeerID,
					"policyRules":  n.PolicyRules,
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
				"egressPeerId": n.EgressPeerID,
				"policyRules":  n.PolicyRules,
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
