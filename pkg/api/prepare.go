package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"peer-wan/pkg/model"
	"peer-wan/pkg/store"
)

type PrepareResponse struct {
	ID             string `json:"id"`
	PublicKey      string `json:"publicKey"`
	PrivateKey     string `json:"privateKey"`
	OverlayIP      string `json:"overlayIp"`
	ListenPort     int    `json:"listenPort"`
	ProvisionToken string `json:"provisionToken"`
	Command        string `json:"command"`
}

func RegisterPrepareRoute(mux *http.ServeMux, store store.NodeStore, planVersion *int64, auth func(r *http.Request) bool, controllerAddr string) {
	mux.HandleFunc("/api/v1/nodes/prepare", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		addr := controllerAddr
		if addr == "" {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			addr = fmt.Sprintf("%s://%s", scheme, r.Host)
		}
		existing, ok, _ := store.GetNode(req.ID)
		var node model.Node
		if ok && existing.ProvisionToken != "" {
			node = existing
		} else {
			priv, err := wgtypes.GeneratePrivateKey()
			if err != nil {
				http.Error(w, "failed to generate key", http.StatusInternalServerError)
				return
			}
			pub := priv.PublicKey()
			overlay := assignOverlay(store)
			token := fmt.Sprintf("pt-%d", time.Now().UnixNano())
			node = model.Node{
				ID:             req.ID,
				PublicKey:      pub.String(),
				PrivateKey:     priv.String(),
				OverlayIP:      overlay,
				ListenPort:     51820,
				ASN:            65000,
				RouterID:       ipWithoutMask(overlay),
				ProvisionToken: token,
			}
			if _, err := store.UpsertNode(node); err != nil {
				http.Error(w, "failed to save node", http.StatusInternalServerError)
				return
			}
		}
		BumpPlanVersion(planVersion)
		cmd := fmt.Sprintf(`curl -fsSL https://raw.githubusercontent.com/NiuStar/peer-wan/refs/heads/main/scripts/agent-install.sh -o /tmp/agent-install.sh && chmod +x /tmp/agent-install.sh && sudo /tmp/agent-install.sh --controller=%s --node-id=%s --provision-token=%s --auto-endpoint=true`,
			addr, req.ID, node.ProvisionToken)
		resp := PrepareResponse{
			ID:             req.ID,
			PublicKey:      node.PublicKey,
			PrivateKey:     node.PrivateKey,
			OverlayIP:      node.OverlayIP,
			ListenPort:     node.ListenPort,
			ProvisionToken: node.ProvisionToken,
			Command:        cmd,
		}
		writeJSON(w, http.StatusOK, resp)
	})
}

func assignOverlay(store store.NodeStore) string {
	nodes, _ := store.ListNodes()
	used := make(map[int]bool)
	for _, n := range nodes {
		ip := ipWithoutMask(n.OverlayIP)
		var third int
		if _, err := fmt.Sscanf(ip, "10.10.%d.", &third); err == nil {
			used[third] = true
		}
	}
	for i := 1; i < 254; i++ {
		if !used[i] {
			return fmt.Sprintf("10.10.%d.1/32", i)
		}
	}
	// fallback
	return "10.10.250.1/32"
}

func ipWithoutMask(cidr string) string {
	if idx := strings.Index(cidr, "/"); idx > 0 {
		return cidr[:idx]
	}
	return cidr
}
