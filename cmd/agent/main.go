package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"peer-wan/pkg/agent"
	"peer-wan/pkg/api"
	"peer-wan/pkg/model"
	"peer-wan/pkg/version"
)

func main() {
	defaultID := os.Getenv("NODE_ID")
	defaultController := os.Getenv("CONTROLLER_ADDR")
	if defaultController == "" {
		defaultController = "http://127.0.0.1:8080"
	}
	defaultToken := os.Getenv("AUTH_TOKEN")
	defaultCA := os.Getenv("CA_FILE")
	defaultProvision := os.Getenv("PROVISION_TOKEN")

	nodeID := flag.String("id", defaultID, "node id (overrides NODE_ID env)")
	showVersion := flag.Bool("v", false, "print version and exit")
	publicKey := flag.String("pub", "stub-public-key", "wireguard public key (placeholder)")
	endpoints := flag.String("endpoints", "127.0.0.1:51820", "comma separated endpoints")
	cidrs := flag.String("cidrs", "10.10.1.0/24", "comma separated CIDRs announced by this node")
	overlayIP := flag.String("overlay-ip", "10.10.1.1/32", "overlay interface IP (wg Address)")
	listenPort := flag.Int("listen-port", 51820, "wireguard listen port")
	privateKey := flag.String("priv", "stub-private-key", "wireguard private key (placeholder)")
	outputDir := flag.String("out", "./out", "directory to write rendered configs")
	iface := flag.String("iface", "wg0", "wireguard interface name")
	asn := flag.Int("asn", 65000, "BGP ASN for FRR config")
	routerID := flag.String("router-id", "", "override BGP router-id (defaults to overlay IP)")
	apply := flag.Bool("apply", false, "attempt to apply configs (wg-quick + vtysh -b)")
	controller := flag.String("controller", defaultController, "controller base URL")
	authToken := flag.String("token", defaultToken, "auth token matching controller --token (env AUTH_TOKEN)")
	caFile := flag.String("ca", defaultCA, "CA file for controller TLS (optional)")
	clientCert := flag.String("cert", "", "client TLS certificate (for mTLS)")
	clientKey := flag.String("key", "", "client TLS key (for mTLS)")
	insecure := flag.Bool("insecure", false, "skip TLS verify for controller (not recommended)")
	healthInterval := flag.Duration("health-interval", 0, "if >0, agent probes peers and posts /health (e.g., 30s)")
	planInterval := flag.Duration("plan-interval", 0, "if >0, poll controller plan and re-render/apply on change (e.g., 30s)")
	provisionToken := flag.String("provision-token", defaultProvision, "one-time provision token from controller (env PROVISION_TOKEN)")
	autoEndpoint := flag.Bool("auto-endpoint", true, "auto-detect endpoint when provision-token is set")
	flag.Parse()

	if *showVersion {
		log.Printf("agent version=%s", version.Build)
		return
	}

	if *nodeID == "" {
		log.Fatal("node id is required (flag --id or env NODE_ID)")
	}
	if *controller == "" {
		log.Fatal("controller base URL is required")
	}

	client, err := buildHTTPClient(*caFile, *clientCert, *clientKey, *insecure)
	if err != nil {
		log.Fatalf("http client build failed: %v", err)
	}

	req := api.NodeRegistrationRequest{
		ID:             *nodeID,
		PublicKey:      *publicKey,
		Endpoints:      splitAndTrim(*endpoints),
		CIDRs:          splitAndTrim(*cidrs),
		OverlayIP:      *overlayIP,
		ListenPort:     *listenPort,
		ASN:            *asn,
		RouterID:       *routerID,
		ProvisionToken: *provisionToken,
	}
	if *provisionToken != "" && *overlayIP == "10.10.1.1/32" {
		req.OverlayIP = ""
	}
	if *autoEndpoint {
		if eps := detectEndpoints(*listenPort); len(eps) > 0 {
			req.Endpoints = eps
		}
	}

	cfg, err := register(client, *controller, *authToken, req)
	if err != nil {
		log.Fatalf("register failed: %v", err)
	}

	selectedOverlay := firstNonEmpty(cfg.OverlayIP, *overlayIP)
	selectedListen := chooseInt(cfg.ListenPort, *listenPort)
	selectedASN := chooseInt(cfg.ASN, *asn)
	selectedRouterID := firstNonEmpty(cfg.RouterID, *routerID, ipFromCIDR(selectedOverlay))
	selectedPriv := firstNonEmpty(cfg.PrivateKey, *privateKey)

	node := model.Node{
		ID:         cfg.ID,
		CIDRs:      cfg.Routes,
		OverlayIP:  selectedOverlay,
		ListenPort: selectedListen,
		ASN:        selectedASN,
		RouterID:   selectedRouterID,
	}
	wgPath, bgpPath, err := agent.RenderAndWrite(*outputDir, *iface, node, cfg.WireGuardPeers, selectedPriv, selectedASN)
	if err != nil {
		log.Fatalf("render/apply failed: %v", err)
	}
	log.Printf("agent version=%s", version.Build)
	log.Printf("configs written: wireguard=%s bgp=%s (apply=%v)", wgPath, bgpPath, *apply)
	if *apply {
		if err := agent.ApplyConfigs(wgPath, *iface, bgpPath); err != nil {
			log.Fatalf("apply failed: %v", err)
		}
		log.Printf("apply succeeded (wg-quick up + vtysh -b)")
	}

	if *healthInterval > 0 {
		agent.StartHealthReporter(client, *controller, *authToken, *provisionToken, cfg.ID, cfg.WireGuardPeers, *healthInterval)
	}

	if *planInterval > 0 {
		agent.StartPlanPoller(client, *controller, *authToken, *provisionToken, cfg.ID, node, *iface, *outputDir, selectedPriv, selectedASN, *apply, *planInterval)
	}

	if *healthInterval > 0 || *planInterval > 0 {
		select {}
	}
}

func register(client *http.Client, controller, token string, req api.NodeRegistrationRequest) (api.NodeConfigResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return api.NodeConfigResponse{}, fmt.Errorf("marshal request: %w", err)
	}
	url := fmt.Sprintf("%s/api/v1/nodes/register", controller)

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return api.NodeConfigResponse{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	if req.ProvisionToken != "" {
		httpReq.Header.Set("X-Provision-Token", req.ProvisionToken)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return api.NodeConfigResponse{}, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return api.NodeConfigResponse{}, fmt.Errorf("controller returned %s body=%s", resp.Status, strings.TrimSpace(string(b)))
	}

	var cfg api.NodeConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return api.NodeConfigResponse{}, fmt.Errorf("decode response: %w", err)
	}

	log.Printf("registered node=%s version=%s routes=%v message=%s", cfg.ID, cfg.ConfigVersion, cfg.Routes, cfg.Message)
	if len(cfg.WireGuardPeers) > 0 {
		log.Printf("peer plan: %+v", cfg.WireGuardPeers)
	} else {
		log.Printf("peer plan empty (expected in stub), controller will populate once topology is enabled")
	}
	return cfg, nil
}

func buildHTTPClient(caFile, certFile, keyFile string, insecure bool) (*http.Client, error) {
	tlsConfig := &tls.Config{InsecureSkipVerify: insecure} //nolint:gosec
	if caFile != "" {
		caCertPool := x509.NewCertPool()
		caData, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read ca file: %w", err)
		}
		caCertPool.AppendCertsFromPEM(caData)
		tlsConfig.RootCAs = caCertPool
	}
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}

func splitAndTrim(s string) []string {
	out := []string{}
	for _, part := range bytes.Split([]byte(s), []byte(",")) {
		p := string(bytes.TrimSpace(part))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func detectEndpoints(listenPort int) []string {
	var eps []string
	// 1) best-effort via UDP dial to discover default egress
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP != nil {
			if isPublic(addr.IP) {
				eps = append(eps, fmt.Sprintf("%s:%d", addr.IP.String(), listenPort))
			}
		}
		_ = conn.Close()
	}
	// 2) public IP services (ipv4/ipv6)
	for _, svc := range []string{
		"http://ipv4.icanhazip.com",
		"http://ipv6.icanhazip.com",
		"https://api.ipify.org",
		"https://ip.sb",
	} {
		if ip := fetchPublicIP(svc); ip != "" {
			if strings.Contains(ip, ":") {
				eps = append(eps, fmt.Sprintf("[%s]:%d", ip, listenPort))
			} else {
				eps = append(eps, fmt.Sprintf("%s:%d", ip, listenPort))
			}
		}
	}
	// 2) enumerate interfaces for public/global addresses
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if !isPublic(ip) {
				continue
			}
			if ip.To4() == nil && strings.Contains(ip.String(), ":") {
				eps = append(eps, fmt.Sprintf("[%s]:%d", ip.String(), listenPort))
			} else {
				eps = append(eps, fmt.Sprintf("%s:%d", ip.String(), listenPort))
			}
		}
	}
	return dedup(eps)
}

func isPrivate(ip net.IP) bool {
	if ip.To4() != nil {
		// 10.0.0.0/8, 172.16/12, 192.168/16, 100.64/10, 169.254/16
		if ip[0] == 10 {
			return true
		}
		if ip[0] == 172 && ip[1]&0xf0 == 16 {
			return true
		}
		if ip[0] == 192 && ip[1] == 168 {
			return true
		}
		if ip[0] == 100 && ip[1]&0xc0 == 0x40 { // 100.64.0.0/10
			return true
		}
		if ip[0] == 169 && ip[1] == 254 {
			return true
		}
	}
	// ULA fc00::/7
	if ip.To16() != nil && ip[0]&0xfe == 0xfc {
		return true
	}
	// loopback
	if ip.IsLoopback() {
		return true
	}
	return false
}

func isPublic(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsGlobalUnicast() && !isPrivate(ip)
}

func dedup(xs []string) []string {
	seen := make(map[string]bool)
	out := []string{}
	for _, x := range xs {
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

func fetchPublicIP(url string) string {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	ip := strings.TrimSpace(string(b))
	if ip == "" {
		return ""
	}
	parsed := net.ParseIP(ip)
	if parsed == nil || !isPublic(parsed) {
		return ""
	}
	return ip
}

func endpointsPrivate(eps []string) bool {
	if len(eps) == 0 {
		return true
	}
	for _, ep := range eps {
		host, _, err := net.SplitHostPort(strings.TrimSpace(ep))
		if err != nil {
			continue
		}
		if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && !isPrivate(ip) {
			return false
		}
	}
	return true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func chooseInt(candidates ...int) int {
	for _, v := range candidates {
		if v > 0 {
			return v
		}
	}
	return 0
}

func ipFromCIDR(cidr string) string {
	if cidr == "" {
		return ""
	}
	if i := strings.Index(cidr, "/"); i > 0 {
		return cidr[:i]
	}
	return cidr
}
