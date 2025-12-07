package api

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"peer-wan/pkg/model"
	"peer-wan/pkg/store"
)

// GeoLocation carries best-effort IP geolocation.
type GeoLocation struct {
	IP      string  `json:"ip,omitempty"`
	Lat     float64 `json:"lat,omitempty"`
	Lng     float64 `json:"lng,omitempty"`
	City    string  `json:"city,omitempty"`
	Country string  `json:"country,omitempty"`
	Source  string  `json:"source,omitempty"`
}

type NodeStatus struct {
	ID        string       `json:"id"`
	OverlayIP string       `json:"overlayIp,omitempty"`
	Location  *GeoLocation `json:"location,omitempty"`
}

type LinkStatus struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	OK         bool    `json:"ok"`
	LatencyMs  int     `json:"latencyMs,omitempty"`
	PacketLoss float64 `json:"packetLoss,omitempty"`
	ProbeIP    string  `json:"probeIp,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

type MeshStatusResponse struct {
	Nodes           []NodeStatus `json:"nodes"`
	Links           []LinkStatus `json:"links"`
	PingIntervalSec int          `json:"pingIntervalSec,omitempty"`
}

func RegisterStatusRoutes(mux *http.ServeMux, st store.NodeStore, auth func(r *http.Request) bool) {
	mux.HandleFunc("/api/v1/status/mesh", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		nodes, err := st.ListNodes()
		if err != nil {
			http.Error(w, "failed to list nodes", http.StatusInternalServerError)
			return
		}
		health, _ := st.ListHealth()

		resp := MeshStatusResponse{
			Nodes:           buildNodeStatuses(nodes),
			Links:           buildLinkStatuses(nodes, health),
			PingIntervalSec: diagIntervalSeconds(st),
		}
		writeJSON(w, http.StatusOK, resp)
	})
}

func buildNodeStatuses(nodes []model.Node) []NodeStatus {
	out := make([]NodeStatus, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, NodeStatus{
			ID:        n.ID,
			OverlayIP: ipWithoutMask(n.OverlayIP),
			Location:  resolveGeoForNode(n),
		})
	}
	return out
}

func buildLinkStatuses(nodes []model.Node, health []model.HealthReport) []LinkStatus {
	healthMap := map[string]model.HealthReport{}
	for _, h := range health {
		healthMap[h.NodeID] = h
	}
	var links []LinkStatus
	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			a := nodes[i]
			b := nodes[j]
			from, to := a.ID, b.ID
			status := LinkStatus{From: from, To: to}
			aip := ipWithoutMask(a.OverlayIP)
			bip := ipWithoutMask(b.OverlayIP)
			if len(a.Endpoints) == 0 || len(b.Endpoints) == 0 {
				status.OK = false
				status.Reason = "missing endpoint"
				links = append(links, status)
				continue
			}
			var latency int
			var loss float64
			var seen bool
			if h, ok := healthMap[from]; ok {
				if ms, ok2 := h.LatencyMs[bip]; ok2 {
					latency = ms
					loss = h.PacketLoss[bip]
					status.ProbeIP = bip
					seen = true
				}
			}
			if !seen {
				if h, ok := healthMap[to]; ok {
					if ms, ok2 := h.LatencyMs[aip]; ok2 {
						latency = ms
						loss = h.PacketLoss[aip]
						status.ProbeIP = aip
						seen = true
					}
				}
			}
			if seen {
				status.LatencyMs = latency
				status.PacketLoss = loss
				status.OK = loss < 100
				if !status.OK {
					status.Reason = "packet loss 100%"
				}
			} else {
				status.OK = false
				status.Reason = "no telemetry"
			}
			links = append(links, status)
		}
	}
	return links
}

// --- Geo lookup with simple in-memory cache ---

var (
	geoCache   = map[string]geoCacheEntry{}
	geoCacheMu sync.Mutex
)

type geoCacheEntry struct {
	loc      *GeoLocation
	expires  time.Time
	lastIP   string
	lastHost string
}

func resolveGeoForNode(n model.Node) *GeoLocation {
	ip := ""
	if len(n.Endpoints) > 0 {
		if host, _, err := net.SplitHostPort(n.Endpoints[0]); err == nil {
			ip = host
		}
	}
	if ip == "" {
		ip = ipWithoutMask(n.OverlayIP)
	}
	if ip == "" || net.ParseIP(ip) == nil {
		return nil
	}
	return resolveGeo(ip)
}

func resolveGeo(ip string) *GeoLocation {
	geoCacheMu.Lock()
	if entry, ok := geoCache[ip]; ok && time.Now().Before(entry.expires) {
		geoCacheMu.Unlock()
		return entry.loc
	}
	geoCacheMu.Unlock()

	loc := fetchGeo(ip)
	geoCacheMu.Lock()
	geoCache[ip] = geoCacheEntry{loc: loc, expires: time.Now().Add(30 * time.Minute)}
	geoCacheMu.Unlock()
	return loc
}

func fetchGeo(ip string) *GeoLocation {
	url := "https://ipapi.co/" + ip + "/json/"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("geo lookup failed for %s: %v", ip, err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil
	}
	var body struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		City      string  `json:"city"`
		Country   string  `json:"country_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil
	}
	return &GeoLocation{
		IP:      ip,
		Lat:     body.Latitude,
		Lng:     body.Longitude,
		City:    body.City,
		Country: body.Country,
		Source:  "ipapi",
	}
}
