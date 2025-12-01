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
func StartPlanPoller(client *http.Client, controller, token, nodeID string, node model.Node, iface, outDir, privateKey string, asn int, apply bool, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		var lastVersion string
		var lastNumeric int64
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// optional: if consul watch build tag present, hook into watch to trigger immediate fetch
		if WatchEnabled() {
			_ = StartConsulWatch(ctx, controller, token, func(v int64) {
				// on change, set lastNumeric to v-1 to force fetch
				if v > lastNumeric {
					lastNumeric = v - 1
				}
			})
		}
		for {
			cfg, err := fetchPlan(client, controller, token, nodeID, lastNumeric)
			if err != nil {
				log.Printf("plan poll failed: %v", err)
			} else {
				if cfg.ConfigVersion != "" && cfg.ConfigVersion == lastVersion {
					<-ticker.C
					continue
				}
				if err := handlePlan(cfg, node, outDir, iface, privateKey, asn, apply); err != nil {
					log.Printf("plan apply failed: %v", err)
				} else {
					lastVersion = cfg.ConfigVersion
					lastNumeric = parseNumericVersion(cfg.ConfigVersion)
				}
			}
			<-ticker.C
		}
	}()
}

func handlePlan(cfg api.NodeConfigResponse, node model.Node, outDir, iface, privateKey string, asn int, apply bool) error {
	wgPath, bgpPath, err := RenderAndWrite(outDir, iface, node, cfg.WireGuardPeers, privateKey, asn)
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

func fetchPlan(client *http.Client, controller, token, nodeID string, waitVersion int64) (api.NodeConfigResponse, error) {
	// first check global plan version
	url := fmt.Sprintf("%s/api/v1/version", controller)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return api.NodeConfigResponse{}, err
	}
	if token != "" {
		req.Header.Set("X-Auth-Token", token)
	}
	resp, err := client.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		var v struct {
			Version int64 `json:"version"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&v)
		if v.Version > waitVersion {
			waitVersion = v.Version
		}
	}
	if resp != nil {
		_ = resp.Body.Close()
	}

	// then fetch plan with waitVersion
	url = fmt.Sprintf("%s/api/v1/plan?nodeId=%s", controller, nodeID)
	if waitVersion > 0 {
		url += fmt.Sprintf("&waitVersion=%d", waitVersion)
	}
	req, err = http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return api.NodeConfigResponse{}, err
	}
	if token != "" {
		req.Header.Set("X-Auth-Token", token)
	}
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
