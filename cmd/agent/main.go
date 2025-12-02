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
	if *provisionToken != "" && *autoEndpoint {
		if ep := detectEndpoint(*listenPort); ep != "" {
			req.Endpoints = []string{ep}
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
		Timeout: 5 * time.Second,
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

func detectEndpoint(listenPort int) string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP != nil {
		return fmt.Sprintf("%s:%d", addr.IP.String(), listenPort)
	}
	return ""
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
