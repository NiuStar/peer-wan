package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"peer-wan/pkg/model"
	"peer-wan/pkg/store"
)

// RegisterTaskRoutes exposes APIs to create/query tasks and dispatch to agents via WS.
func RegisterTaskRoutes(mux *http.ServeMux, st store.NodeStore, auth func(r *http.Request) bool, hub *WSHub) {
	if hub == nil {
		return
	}
	mux.HandleFunc("/api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPost:
			var req struct {
				NodeID        string                 `json:"nodeId"`
				Targets       []string               `json:"targets"`
				Type          string                 `json:"type"` // policy_apply / policy_diag / verify
				VerifyTargets []string               `json:"verifyTargets,omitempty"`
				Data          map[string]interface{} `json:"data,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Type == "" {
				http.Error(w, "invalid payload", http.StatusBadRequest)
				return
			}
			targets := req.Targets
			if len(targets) == 0 && req.NodeID != "" {
				targets = []string{req.NodeID}
			}
			if len(targets) == 0 {
				http.Error(w, "targets required", http.StatusBadRequest)
				return
			}
			taskID := uuid.NewString()
			task := model.Task{
				ID:            taskID,
				NodeID:        req.NodeID,
				Targets:       targets,
				Type:          req.Type,
				Status:        "running",
				OverallStatus: "running",
				Steps:         []model.TaskStep{{Name: "dispatch", Status: "running", Timestamp: time.Now(), Message: "sending to agents"}},
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			}
			_ = st.SaveTask(task)
			for _, t := range targets {
				payload := map[string]interface{}{"taskId": taskID, "type": req.Type}
				if len(req.VerifyTargets) > 0 {
					payload["verifyTargets"] = req.VerifyTargets
				}
				if len(req.Data) > 0 {
					payload["data"] = req.Data
				}
				hub.Send(t, WSMessage{Type: "task", NodeID: t, Payload: payload})
			}
			writeJSON(w, http.StatusOK, task)
		case http.MethodGet:
			nodeID := r.URL.Query().Get("nodeId")
			items, err := st.ListTasks(nodeID, 50)
			if err != nil {
				http.Error(w, "failed to list", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
