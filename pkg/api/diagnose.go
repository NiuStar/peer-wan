package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"peer-wan/pkg/model"
	"peer-wan/pkg/store"
)

// DiagnoseResult captures a single check outcome.
type DiagnoseResult struct {
	Check    string `json:"check"`
	Status   string `json:"status"`   // ok/warn/fail/info
	Severity string `json:"severity"` // mirrors status for UI coloring
	Detail   string `json:"detail"`
}

type DiagnoseResponse struct {
	NodeID    string           `json:"nodeId"`
	Summary   string           `json:"summary"`
	Results   []DiagnoseResult `json:"results"`
	Timestamp time.Time        `json:"timestamp"`
}

func RegisterDiagnoseRoutes(mux *http.ServeMux, st store.NodeStore, auth func(r *http.Request) bool) {
	mux.HandleFunc("/api/v1/diagnose", func(w http.ResponseWriter, r *http.Request) {
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
		resp := diagnoseNode(st, nodeID)
		writeJSON(w, http.StatusOK, resp)
	})
}

func diagnoseNode(st store.NodeStore, nodeID string) DiagnoseResponse {
	now := time.Now()
	results := []DiagnoseResult{}
	summary := "全部检查完成"

	node, ok, err := st.GetNode(nodeID)
	if err != nil || !ok {
		return DiagnoseResponse{NodeID: nodeID, Summary: "节点不存在", Results: []DiagnoseResult{{Check: "节点存在", Status: "fail", Severity: "fail", Detail: "未找到节点"}}, Timestamp: now}
	}

	diagCfg := loadSettingsOrDefault(st)
	interval := 3 * time.Second
	if diagCfg.Diag.PingInterval != "" {
		if d, err := time.ParseDuration(diagCfg.Diag.PingInterval); err == nil && d > 0 {
			interval = d
		}
	}
	staleThreshold := interval * 5
	if staleThreshold < 15*time.Second {
		staleThreshold = 15 * time.Second
	}

	// Fetch latest health
	healthList, _ := st.ListHealth()
	var health model.HealthReport
	var hasHealth bool
	for _, h := range healthList {
		if h.NodeID == nodeID {
			health = h
			hasHealth = true
			break
		}
	}

	if !hasHealth {
		results = append(results, DiagnoseResult{Check: "Agent 心跳", Status: "fail", Severity: "fail", Detail: "未收到健康上报，可能 agent 未安装/未运行"})
		summary = "未收到健康上报"
		return DiagnoseResponse{NodeID: nodeID, Summary: summary, Results: results, Timestamp: now}
	}

	age := now.Sub(health.Timestamp)
	if age > staleThreshold {
		results = append(results, DiagnoseResult{Check: "Agent 心跳", Status: "warn", Severity: "warn", Detail: fmt.Sprintf("健康上报已滞后 %.0fs，可能 agent 停止或网络阻断", age.Seconds())})
	} else {
		results = append(results, DiagnoseResult{Check: "Agent 心跳", Status: "ok", Severity: "ok", Detail: "上报正常"})
	}

	if len(node.Endpoints) == 0 {
		results = append(results, DiagnoseResult{Check: "Endpoint 配置", Status: "warn", Severity: "warn", Detail: "节点未配置 endpoint，可能无法被其他节点连接"})
	} else {
		results = append(results, DiagnoseResult{Check: "Endpoint 配置", Status: "ok", Severity: "ok", Detail: strings.Join(node.Endpoints, ", ")})
	}

	plan, planOK, _ := st.GetPlan(nodeID)
	if !planOK || len(plan.Peers) == 0 {
		results = append(results, DiagnoseResult{Check: "拓扑计划", Status: "warn", Severity: "warn", Detail: "未找到计划或无任何 peer，检查 controller 拓扑计算"})
	}

	// WireGuard reachability heuristics
	if len(plan.Peers) > 0 {
		missing := []string{}
		lossBlocked := []string{}
		for _, p := range plan.Peers {
			overlay := ipWithoutMask(p.AllowedIPs[0])
			ms, ok := health.LatencyMs[overlay]
			loss := health.PacketLoss[overlay]
			if !ok {
				missing = append(missing, p.ID)
				continue
			}
			if loss >= 100 {
				lossBlocked = append(lossBlocked, fmt.Sprintf("%s(loss=%.0f%%)", p.ID, loss))
			}
			_ = ms
		}
		if len(lossBlocked) > 0 {
			results = append(results, DiagnoseResult{Check: "WireGuard/防火墙", Status: "fail", Severity: "fail", Detail: "探测到对端 100% 丢包: " + strings.Join(lossBlocked, ", ")})
		}
		if len(missing) > 0 {
			results = append(results, DiagnoseResult{Check: "WireGuard 链路", Status: "warn", Severity: "warn", Detail: "未收到对这些节点的延迟数据，可能 WG 未握手或防火墙阻断: " + strings.Join(missing, ", ")})
		} else {
			results = append(results, DiagnoseResult{Check: "WireGuard 链路", Status: "ok", Severity: "ok", Detail: "已收到对端延迟/丢包数据"})
		}
	}

	// FRR check
	if len(health.FRRState) > 0 {
		bad := []string{}
		for nbr, st := range health.FRRState {
			if strings.ToLower(st) != "established" {
				bad = append(bad, fmt.Sprintf("%s=%s", nbr, st))
			}
		}
		if len(bad) > 0 {
			results = append(results, DiagnoseResult{Check: "FRR 邻居", Status: "warn", Severity: "warn", Detail: "邻居状态异常: " + strings.Join(bad, "; ")})
		} else {
			results = append(results, DiagnoseResult{Check: "FRR 邻居", Status: "ok", Severity: "ok", Detail: "BGP 邻居均已 Established"})
		}
	}

	// summarize severity
	summary = highestSeverity(results)
	return DiagnoseResponse{NodeID: nodeID, Summary: summary, Results: results, Timestamp: now}
}

func highestSeverity(results []DiagnoseResult) string {
	level := map[string]int{"fail": 3, "warn": 2, "ok": 1, "info": 0}
	maxL := 0
	for _, r := range results {
		if l := level[r.Severity]; l > maxL {
			maxL = l
		}
	}
	for s, l := range level {
		if l == maxL {
			switch s {
			case "fail":
				return "发现阻断/错误"
			case "warn":
				return "存在警告，请排查"
			default:
				return "检查通过"
			}
		}
	}
	return "检查完成"
}
