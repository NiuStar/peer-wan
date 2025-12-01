package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
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
	flag.Parse()

	if *nodeID == "" {
		log.Fatal("node id is required (flag --id or env NODE_ID)")
	}
	if *controller == "" {
		log.Fatal("controller base URL is required")
	}

	req := api.NodeRegistrationRequest{
		ID:         *nodeID,
		PublicKey:  *publicKey,
		Endpoints:  splitAndTrim(*endpoints),
		CIDRs:      splitAndTrim(*cidrs),
		OverlayIP:  *overlayIP,
		ListenPort: *listenPort,
		ASN:        *asn,
		RouterID:   *routerID,
	}

	client, err := buildHTTPClient(*caFile, *clientCert, *clientKey, *insecure)
	if err != nil {
		log.Fatalf("http client build failed: %v", err)
	}

	cfg, err := register(client, *controller, *authToken, req)
	if err != nil {
		log.Fatalf("register failed: %v", err)
	}

	node := model.Node{
		ID:         cfg.ID,
		CIDRs:      cfg.Routes,
		OverlayIP:  *overlayIP,
		ListenPort: *listenPort,
		ASN:        *asn,
		RouterID:   *routerID,
	}
	wgPath, bgpPath, err := agent.RenderAndWrite(*outputDir, *iface, node, cfg.WireGuardPeers, *privateKey, *asn)
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
		agent.StartHealthReporter(client, *controller, *authToken, cfg.ID, cfg.WireGuardPeers, *healthInterval)
	}

	if *planInterval > 0 {
		agent.StartPlanPoller(client, *controller, *authToken, cfg.ID, node, *iface, *outputDir, *privateKey, *asn, *apply, *planInterval)
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
		httpReq.Header.Set("X-Auth-Token", token)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return api.NodeConfigResponse{}, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return api.NodeConfigResponse{}, fmt.Errorf("controller returned %s", resp.Status)
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
