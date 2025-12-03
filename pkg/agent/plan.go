package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"peer-wan/pkg/api"
	"peer-wan/pkg/model"
)

// StartPlanPoller periodically fetches a dynamic plan from the controller and applies it.
func StartPlanPoller(client *http.Client, controller, authToken, provisionToken, nodeID string, node model.Node, iface, outDir, privateKey string, asn int, apply bool, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		var lastVersion string
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// optional: if consul watch build tag present, hook into watch to trigger immediate fetch
		if WatchEnabled() {
			_ = StartConsulWatch(ctx, controller, authToken, func(v int64) {
				if v > 0 {
					lastVersion = ""
				}
			})
		}
		for {
			cfg, err := fetchPlan(controller, authToken, provisionToken, nodeID)
			if err != nil {
				log.Printf("plan poll failed: %v", err)
			} else if cfg.ConfigVersion != "" && cfg.ConfigVersion != lastVersion {
				if err := handlePlan(cfg, node, outDir, iface, privateKey, asn, apply); err != nil {
					log.Printf("plan apply failed: %v", err)
				} else {
					lastVersion = cfg.ConfigVersion
				}
			}
			<-ticker.C
		}
	}()
}

func handlePlan(cfg api.NodeConfigResponse, node model.Node, outDir, iface, privateKey string, asn int, apply bool) error {
	n := node
	if cfg.OverlayIP != "" {
		n.OverlayIP = cfg.OverlayIP
	}
	if cfg.ListenPort > 0 {
		n.ListenPort = cfg.ListenPort
	}
	if cfg.ASN > 0 {
		n.ASN = cfg.ASN
	}
	if cfg.RouterID != "" {
		n.RouterID = cfg.RouterID
	}
	if len(cfg.Routes) > 0 {
		n.CIDRs = cfg.Routes
	}
	if len(cfg.PeerEndpoints) > 0 {
		n.PeerEndpoints = cfg.PeerEndpoints
	}
	nextASN := asn
	if n.ASN > 0 {
		nextASN = n.ASN
	}
	wgPath, bgpPath, err := RenderAndWrite(outDir, iface, n, cfg.WireGuardPeers, privateKey, nextASN)
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}
	log.Printf("plan updated version=%s; configs written wg=%s bgp=%s", cfg.ConfigVersion, wgPath, bgpPath)
	if apply {
		if err := ApplyConfigs(wgPath, iface, bgpPath); err != nil {
			return fmt.Errorf("apply: %w", err)
		}
		log.Printf("plan applied (wg-quick + vtysh)")
	}
	return nil
}

func fetchPlan(controller, authToken, provisionToken, nodeID string) (api.NodeConfigResponse, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	// first check global plan version
	url := fmt.Sprintf("%s/api/v1/version", controller)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return api.NodeConfigResponse{}, err
	}
	setAuth(req, authToken, provisionToken)
	resp, err := client.Do(req)
	if err != nil {
		return api.NodeConfigResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return api.NodeConfigResponse{}, fmt.Errorf("version fetch failed: %s", resp.Status)
	}

	// fetch plan without long wait
	url = fmt.Sprintf("%s/api/v1/plan?nodeId=%s", controller, nodeID)
	req, err = http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return api.NodeConfigResponse{}, err
	}
	setAuth(req, authToken, provisionToken)
	resp, err = client.Do(req)
	if err != nil {
		return api.NodeConfigResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return api.NodeConfigResponse{}, fmt.Errorf("plan fetch failed: %s", resp.Status)
	}
	var cfg api.NodeConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return api.NodeConfigResponse{}, err
	}
	return cfg, nil
}

func parseNumericVersion(cv string) int64 {
	if strings.HasPrefix(cv, "dynamic-v") {
		v := strings.TrimPrefix(cv, "dynamic-v")
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 0
}
