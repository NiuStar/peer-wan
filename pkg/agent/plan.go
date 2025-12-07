package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"peer-wan/pkg/api"
	"peer-wan/pkg/model"
	"peer-wan/pkg/policy"
)

var (
	agentWS     *wsClient
	wsStateMu   sync.RWMutex
	latestCfg   api.NodeConfigResponse
	latestNode  model.Node
	currentTask string
	wsCtx       struct {
		iface      string
		outDir     string
		private    string
		asn        int
		apply      bool
		controller string
		auth       string
		provision  string
		nodeID     string
	}
)

func wsSend(msgType string, payload interface{}) {
	if agentWS == nil {
		return
	}
	agentWS.send(map[string]interface{}{
		"type":    msgType,
		"payload": payload,
	})
}

// wsLog buffers an agent-side log line for upstream WS.
func wsLog(format string, args ...interface{}) {
	if agentWS == nil {
		return
	}
	if len(args) == 0 {
		agentWS.pushLog(format)
		return
	}
	agentWS.pushLog(fmt.Sprintf(format, args...))
}

// StartPlanPoller periodically fetches a dynamic plan from the controller and applies it.
func StartPlanPoller(client *http.Client, controller, authToken, provisionToken, nodeID string, node model.Node, iface, outDir, privateKey string, asn int, apply bool, interval time.Duration) {
	wsStateMu.Lock()
	wsCtx.iface = iface
	wsCtx.outDir = outDir
	wsCtx.private = privateKey
	wsCtx.asn = asn
	wsCtx.apply = apply
	wsCtx.controller = controller
	wsCtx.auth = authToken
	wsCtx.provision = provisionToken
	wsCtx.nodeID = nodeID
	latestNode = node
	wsStateMu.Unlock()
	agentWS = newWSClient(controller, nodeID, authToken, provisionToken)
	if agentWS != nil {
		agentWS.on("command", func(payload map[string]interface{}) { handleWSCommand(payload, client) })
		agentWS.on("plan", handleWSPlan)
		agentWS.on("task", func(p map[string]interface{}) { handleWSTask(p, client) })
		agentWS.start()
	}
	// WS 模式：禁用 HTTP 轮询，仅定期自愈
	if interval > 0 {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				wsStateMu.RLock()
				cfg := latestCfg
				currentNode := latestNode
				wsStateMu.RUnlock()
				if apply && cfg.ConfigVersion != "" {
					if err := ensureRuntimeState(cfg, currentNode, iface, asn, outDir, privateKey, apply, client, controller, authToken, provisionToken); err != nil {
						log.Printf("runtime ensure failed: %v", err)
					}
				}
				<-ticker.C
			}
		}()
	}
}

func handlePlan(cfg api.NodeConfigResponse, node model.Node, outDir, iface, privateKey string, asn int, apply bool, client *http.Client, controller, authToken, provisionToken string) (model.Node, error) {
	n, nextASN := mergePlanIntoNode(node, cfg, asn)
	wsStateMu.Lock()
	latestCfg = cfg
	latestNode = n
	wsStateMu.Unlock()
	log.Printf("apply plan node=%s version=%s egress=%s rules=%d peers=%d", cfg.ID, cfg.ConfigVersion, cfg.EgressPeerID, len(cfg.PolicyRules), len(cfg.WireGuardPeers))
	reportPolicyStatus(client, controller, authToken, provisionToken, n.ID, cfg.ConfigVersion, "applying", "下发策略，准备写入配置", nil)
	wgPath, bgpPath, err := RenderAndWrite(outDir, iface, n, cfg.WireGuardPeers, privateKey, nextASN)
	if err != nil {
		reportPolicyStatus(client, controller, authToken, provisionToken, n.ID, cfg.ConfigVersion, "failed", fmt.Sprintf("渲染失败: %v", err), nil)
		return node, fmt.Errorf("render: %w", err)
	}
	log.Printf("plan updated version=%s; configs written wg=%s bgp=%s", cfg.ConfigVersion, wgPath, bgpPath)
	if apply {
		if err := ApplyConfigs(wgPath, iface, bgpPath); err != nil {
			reportPolicyStatus(client, controller, authToken, provisionToken, n.ID, cfg.ConfigVersion, "failed", fmt.Sprintf("应用失败: %v", err), nil)
			return n, fmt.Errorf("apply: %w", err)
		}
		log.Printf("plan applied (wg-quick + vtysh)")
	}
	reportPolicyStatus(client, controller, authToken, provisionToken, n.ID, cfg.ConfigVersion, "success", "策略配置已应用", []string{"wg+frr+iptables已刷新"})
	wsLog("apply plan success version=%s", cfg.ConfigVersion)
	runPolicyDiag(client, controller, authToken, provisionToken, n, cfg.ConfigVersion, iface, cfg.WireGuardPeers)
	return n, nil
}

// ensureRuntimeState reapplies configs/NAT/routes even when plan version stays the same.
// This helps auto-heal when users manually delete iptables/ip rules or FRR config.
func ensureRuntimeState(cfg api.NodeConfigResponse, base model.Node, iface string, asn int, outDir, privateKey string, apply bool, client *http.Client, controller, authToken, provisionToken string) error {
	n, nextASN := mergePlanIntoNode(base, cfg, asn)
	reportPolicyStatus(client, controller, authToken, provisionToken, n.ID, cfg.ConfigVersion, "checking", "自检策略，修复缺失规则", nil)
	wgPath, bgpPath, err := RenderAndWrite(outDir, iface, n, cfg.WireGuardPeers, privateKey, nextASN)
	if err != nil {
		reportPolicyStatus(client, controller, authToken, provisionToken, n.ID, cfg.ConfigVersion, "failed", fmt.Sprintf("自检渲染失败: %v", err), nil)
		return fmt.Errorf("render (runtime ensure): %w", err)
	}
	if apply {
		if err := ApplyConfigs(wgPath, iface, bgpPath); err != nil {
			reportPolicyStatus(client, controller, authToken, provisionToken, n.ID, cfg.ConfigVersion, "failed", fmt.Sprintf("自检应用失败: %v", err), nil)
			return fmt.Errorf("apply (runtime ensure): %w", err)
		}
	}
	reportPolicyStatus(client, controller, authToken, provisionToken, n.ID, cfg.ConfigVersion, "success", "自检完成，策略已同步", nil)
	runPolicyDiag(client, controller, authToken, provisionToken, n, cfg.ConfigVersion, iface, cfg.WireGuardPeers)
	return nil
}

// handleWSCommand executes controller-pushed commands via websocket.
func handleWSCommand(payload map[string]interface{}, client *http.Client) {
	action, _ := payload["action"].(string)
	wsStateMu.RLock()
	cfg := latestCfg
	n := latestNode
	ctx := wsCtx
	wsStateMu.RUnlock()
	log.Printf("ws command received action=%s node=%s", action, n.ID)
	wsLog("ws command %s", action)
	switch action {
	case "diag":
		runPolicyDiag(client, ctx.controller, ctx.auth, ctx.provision, n, cfg.ConfigVersion, ctx.iface, cfg.WireGuardPeers)
	case "install":
		// re-ensure current plan
		_ = ensureRuntimeState(cfg, n, ctx.iface, ctx.asn, ctx.outDir, ctx.private, ctx.apply, client, ctx.controller, ctx.auth, ctx.provision)
	case "verify":
		targets := collectVerifyTargets(cfg.PolicyRules)
		if err := runCurlVerify(targets); err != nil {
			reportPolicyStatus(client, ctx.controller, ctx.auth, ctx.provision, n.ID, cfg.ConfigVersion, "failed", err.Error(), nil)
		} else {
			reportPolicyStatus(client, ctx.controller, ctx.auth, ctx.provision, n.ID, cfg.ConfigVersion, "success", "验证通过", nil)
		}
	case "script":
		cmd, _ := payload["cmd"].(string)
		if cmd != "" {
			out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
			status := "success"
			msg := "脚本执行完成"
			if err != nil {
				status = "failed"
				msg = fmt.Sprintf("脚本错误: %v", err)
			}
			reportPolicyStatus(client, ctx.controller, ctx.auth, ctx.provision, n.ID, cfg.ConfigVersion, status, msg, []string{string(out)})
		}
	default:
		log.Printf("ws command unknown: %s", action)
	}
}

// handleWSPlan applies a plan pushed via WS (fully替代轮询).
func handleWSPlan(payload map[string]interface{}) {
	wsStateMu.RLock()
	ctx := wsCtx
	wsStateMu.RUnlock()
	b, _ := json.Marshal(payload)
	var cfg api.NodeConfigResponse
	if err := json.Unmarshal(b, &cfg); err != nil {
		log.Printf("ws plan decode failed: %v", err)
		return
	}
	if cfg.ID == "" {
		cfg.ID = ctx.nodeID
	}
	log.Printf("ws plan received version=%s peers=%d rules=%d", cfg.ConfigVersion, len(cfg.WireGuardPeers), len(cfg.PolicyRules))
	if _, err := handlePlan(cfg, latestNode, ctx.outDir, ctx.iface, ctx.private, ctx.asn, ctx.apply, nil, ctx.controller, ctx.auth, ctx.provision); err != nil {
		log.Printf("ws plan apply failed: %v", err)
	}
}

// handleWSTask drives a multi-step task pipeline for policy apply/diagnose.
func handleWSTask(payload map[string]interface{}, client *http.Client) {
	wsStateMu.RLock()
	ctx := wsCtx
	cfg := latestCfg
	n := latestNode
	wsStateMu.RUnlock()
	taskID, _ := payload["taskId"].(string)
	taskType, _ := payload["type"].(string)
	if taskID == "" || taskType == "" {
		return
	}
	currentTask = taskID
	verifyTargets := toStringSlice(payload["verifyTargets"])
	if len(verifyTargets) == 0 {
		verifyTargets = collectVerifyTargets(cfg.PolicyRules)
	}
	step := func(name, status, msg string) {
		wsSend("task_step", map[string]interface{}{
			"taskId": taskID, "nodeId": n.ID, "name": name, "status": status, "message": msg, "ts": time.Now().Unix(),
		})
		wsLog("task %s %s: %s", taskID, name, msg)
	}
	step("environment_check", "running", "检查环境")
	runPolicyDiag(client, ctx.controller, ctx.auth, ctx.provision, n, cfg.ConfigVersion, ctx.iface, cfg.WireGuardPeers)
	step("environment_check", "success", "环境检查完成")

	switch taskType {
	case "policy_diag":
		step("diag", "running", "策略诊断")
		runPolicyDiag(client, ctx.controller, ctx.auth, ctx.provision, n, cfg.ConfigVersion, ctx.iface, cfg.WireGuardPeers)
		step("diag", "success", "诊断完成")
	case "verify":
		step("verify", "running", "出口验证")
		if err := runCurlVerify(verifyTargets); err != nil {
			step("verify", "fail", err.Error())
			return
		}
		step("verify", "success", "目标验证通过")
	default:
		step("apply", "running", "应用策略")
		if _, err := handlePlan(cfg, n, ctx.outDir, ctx.iface, ctx.private, ctx.asn, ctx.apply, client, ctx.controller, ctx.auth, ctx.provision); err != nil {
			step("apply", "fail", err.Error())
			return
		}
		step("apply", "success", "策略应用完成")

		step("self_test", "running", "自检验证")
		runPolicyDiag(client, ctx.controller, ctx.auth, ctx.provision, n, cfg.ConfigVersion, ctx.iface, cfg.WireGuardPeers)
		step("self_test", "success", "自检完成")

		// final verify: curl -4 on rule prefixes/domains
		step("verify", "running", "出口验证")
		if err := runCurlVerify(verifyTargets); err != nil {
			step("verify", "fail", err.Error())
			return
		}
		step("verify", "success", "curl 验证通过")
	}

	step("complete", "success", "任务完成")
	currentTask = ""
}

// mergePlanIntoNode combines controller config with local defaults for reuse.
func mergePlanIntoNode(node model.Node, cfg api.NodeConfigResponse, asn int) (model.Node, int) {
	if cfg.GeoIPConfig != nil {
		policy.SetConfig(model.GeoIPConfig{
			CacheDir: cfg.GeoIPConfig.CacheDir,
			SourceV4: cfg.GeoIPConfig.SourceV4,
			SourceV6: cfg.GeoIPConfig.SourceV6,
			CacheTTL: cfg.GeoIPConfig.CacheTTL,
		})
	}
	n := node
	if cfg.OverlayIP != "" {
		n.OverlayIP = cfg.OverlayIP
	}
	if cfg.ListenPort > 0 {
		n.ListenPort = cfg.ListenPort
	} else if n.ListenPort == 0 {
		// WG over WSS 默认监听 8082
		n.ListenPort = 8082
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
	if cfg.EgressPeerID != "" {
		n.EgressPeerID = cfg.EgressPeerID
	}
	if len(cfg.PolicyRules) > 0 {
		n.PolicyRules = cfg.PolicyRules
	}
	n.DefaultRoute = cfg.DefaultRoute
	if len(cfg.BypassCIDRs) > 0 {
		n.BypassCIDRs = cfg.BypassCIDRs
	}
	if cfg.DefaultRouteNextHop != "" {
		n.DefaultRouteNextHop = cfg.DefaultRouteNextHop
	}
	nextASN := asn
	if n.ASN > 0 {
		nextASN = n.ASN
	}
	return n, nextASN
}

func reportPolicyStatus(client *http.Client, controller, authToken, provisionToken, nodeID, version, status, message string, logs []string) {
	if client == nil || controller == "" || nodeID == "" || status == "" {
		return
	}
	payload := model.PolicyInstallLog{
		NodeID:    nodeID,
		Version:   version,
		Status:    status,
		Message:   message,
		Logs:      logs,
		Timestamp: time.Now(),
	}
	if agentWS != nil {
		agentWS.send(map[string]interface{}{
			"type":    "install_status",
			"nodeId":  nodeID,
			"payload": payload,
		})
	}
	url := fmt.Sprintf("%s/api/v1/policy/status", controller)
	if err := postJSON(client, url, authToken, provisionToken, payload); err != nil {
		log.Printf("report policy status failed: %v", err)
	}
}

// runPolicyDiag performs best-effort local checks and reports to controller.
func runPolicyDiag(client *http.Client, controller, authToken, provisionToken string, node model.Node, version, iface string, peers []model.Peer) {
	if client == nil || controller == "" {
		return
	}
	checks := []model.PolicyDiagCheck{}
	wsLog("diag start version=%s", version)
	add := func(name, status, detail string) {
		checks = append(checks, model.PolicyDiagCheck{Name: name, Status: status, Detail: detail})
	}

	// WireGuard iface
	if ifaceExists(iface) {
		add("wg接口", "ok", iface+" 存在")
	} else {
		add("wg接口", "fail", iface+" 不存在")
	}

	// ip addr
	if out, err := exec.Command("ip", "addr", "show", iface).CombinedOutput(); err == nil {
		add("ip addr", "ok", strings.TrimSpace(string(out)))
	} else {
		add("ip addr", "fail", err.Error())
	}

	// iptables nat rule
	if err := exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING", "-s", defaultOverlayCIDR, "-o", iface, "-j", "MASQUERADE").Run(); err == nil {
		add("NAT MASQUERADE", "ok", "存在 POSTROUTING 掩码规则")
	} else {
		add("NAT MASQUERADE", "warn", "缺少 POSTROUTING 掩码规则")
	}

	// forward rules
	if err := exec.Command("iptables", "-C", "FORWARD", "-i", iface, "-j", "ACCEPT").Run(); err == nil {
		add("FORWARD 允许", "ok", "存在 FORWARD 允许规则")
	} else {
		add("FORWARD 允许", "warn", "缺少 FORWARD 允许规则")
	}

	// ip_forward
	if out, err := exec.Command("sysctl", "-n", "net.ipv4.ip_forward").CombinedOutput(); err == nil && strings.TrimSpace(string(out)) == "1" {
		add("转发开关", "ok", "net.ipv4.ip_forward=1")
	} else {
		add("转发开关", "fail", "net.ipv4.ip_forward 未开启")
	}

	// policy route table 100
	if out, err := exec.Command("ip", "route", "show", "table", "100").CombinedOutput(); err == nil {
		add("策略路由表100", "ok", strings.TrimSpace(string(out)))
	} else {
		add("策略路由表100", "warn", err.Error())
	}

	// frr neighbors
	frrState := readFRRNeighbors()
	if len(frrState) == 0 {
		add("FRR 邻居", "warn", "未获取到邻居状态")
	} else {
		bad := []string{}
		for nbr, st := range frrState {
			if strings.ToLower(st) != "established" {
				bad = append(bad, nbr+"="+st)
			}
		}
		if len(bad) == 0 {
			add("FRR 邻居", "ok", "均已 Established")
		} else {
			add("FRR 邻居", "warn", strings.Join(bad, "; "))
		}
	}

	// peers existence
	if len(peers) > 0 {
		add("WireGuard peers", "ok", fmt.Sprintf("peers=%d", len(peers)))
	} else {
		add("WireGuard peers", "warn", "未下发任何 peer")
	}

	summary := "策略检查通过"
	for _, c := range checks {
		if c.Status == "fail" {
			summary = "检测到错误"
			break
		}
		if c.Status == "warn" && summary == "策略检查通过" {
			summary = "存在警告"
		}
	}

	report := model.PolicyDiagReport{
		NodeID:    node.ID,
		Summary:   summary,
		Checks:    checks,
		Timestamp: time.Now(),
	}
	if agentWS != nil {
		agentWS.send(map[string]interface{}{
			"type":    "diag_result",
			"nodeId":  node.ID,
			"payload": report,
		})
	}
	url := fmt.Sprintf("%s/api/v1/policy/diag", controller)
	if err := postJSON(client, url, authToken, provisionToken, report); err != nil {
		log.Printf("report policy diag failed: %v", err)
	}
	wsLog("diag finish summary=%s checks=%d", summary, len(checks))
}

func toStringSlice(v interface{}) []string {
	out := []string{}
	switch t := v.(type) {
	case []string:
		return t
	case []interface{}:
		for _, x := range t {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
	}
	return out
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
