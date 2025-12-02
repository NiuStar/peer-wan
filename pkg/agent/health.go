package agent

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"peer-wan/pkg/model"
)

// StartHealthReporter launches a goroutine to periodically probe peers and report health.
// If interval <=0, it is a no-op. Ping is best-effort and tolerant to failures.
func StartHealthReporter(client *http.Client, controller, authToken, provisionToken, nodeID string, peers []model.Peer, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := reportOnce(client, controller, authToken, provisionToken, nodeID, peers); err != nil {
				log.Printf("health report failed: %v", err)
			}
			<-ticker.C
		}
	}()
}

func reportOnce(client *http.Client, controller, authToken, provisionToken, nodeID string, peers []model.Peer) error {
	latency := map[string]int{}
	loss := map[string]float64{}
	for _, p := range peers {
		ip := peerOverlayIP(p)
		if ip == "" {
			continue
		}
		ms, pct, err := ping(ip, 1*time.Second)
		if err != nil {
			continue
		}
		latency[ip] = int(ms)
		loss[ip] = pct
	}
	frrState := readFRRNeighbors()
	report := model.HealthReport{
		NodeID:     nodeID,
		Status:     "up",
		LatencyMs:  latency,
		PacketLoss: loss,
		FRRState:   frrState,
	}
	return postJSON(client, controller+"/api/v1/health", authToken, provisionToken, report)
}

func peerOverlayIP(p model.Peer) string {
	for _, ip := range p.AllowedIPs {
		if strings.Contains(ip, "/") {
			return ip
		}
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

// ping tries to ICMP via system ping, falls back to TCP connect if ping missing.
// Returns latency ms and packet loss percentage.
func ping(target string, timeout time.Duration) (float64, float64, error) {
	cmd := exec.Command("ping", "-c", "3", "-W", "1", target)
	out, err := cmd.CombinedOutput()
	if err == nil {
		lat := parsePingLatency(string(out))
		loss := parsePingLoss(string(out))
		return lat, loss, nil
	}
	// fallback: TCP connect on 80 (best effort)
	start := time.Now()
	conn, errDial := net.DialTimeout("tcp", net.JoinHostPort(target, "80"), timeout)
	if errDial != nil {
		return 0, 100, errDial
	}
	_ = conn.Close()
	return float64(time.Since(start).Milliseconds()), 0, nil
}

// readFRRNeighbors best-effort parses "show bgp summary" to map neighbor -> state.
func readFRRNeighbors() map[string]string {
	out := make(map[string]string)
	if b, err := exec.Command("vtysh", "-c", "show bgp summary json").Output(); err == nil {
		out = parseFRRJSON(string(b))
	} else if b, err := exec.Command("vtysh", "-c", "show bgp summary").Output(); err == nil {
		lines := strings.Split(string(b), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) < 6 || net.ParseIP(fields[0]) == nil {
				continue
			}
			state := fields[len(fields)-1]
			out[fields[0]] = state
		}
	}
	return out
}

var pingLossRe = regexp.MustCompile(`([0-9.]+)% packet loss`)
var pingRttRe = regexp.MustCompile(`= ([0-9.]+)/`)

func parsePingLoss(s string) float64 {
	m := pingLossRe.FindStringSubmatch(s)
	if len(m) == 2 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			return v
		}
	}
	return 0
}

func parsePingLatency(s string) float64 {
	m := pingRttRe.FindStringSubmatch(s)
	if len(m) == 2 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			return v
		}
	}
	return 0
}

func parseFRRJSON(body string) map[string]string {
	type summary struct {
		Neighbors map[string]struct {
			State string `json:"state"`
		} `json:"neighbors"`
	}
	out := make(map[string]string)
	var s summary
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		return out
	}
	for k, v := range s.Neighbors {
		out[k] = v.State
	}
	return out
}
