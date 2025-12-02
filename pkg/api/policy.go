package api

import (
	"encoding/json"
	"net/http"

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
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req PolicyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NodeID == "" {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		if err := store.UpdatePolicy(req.NodeID, req.EgressPeer, req.PolicyRules); err != nil {
			http.Error(w, "failed to update policy", http.StatusInternalServerError)
			return
		}
		BumpPlanVersion(planVersion)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}
