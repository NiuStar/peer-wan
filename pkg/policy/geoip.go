package policy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"peer-wan/pkg/model"
)

var (
	geoIPCacheDir = envDefault("GEOIP_CACHE_DIR", defaultCacheDir())
	geoIPv4Source = envDefault("GEOIP_SOURCE_V4", defaultSourceV4())
	geoIPv6Source = envDefault("GEOIP_SOURCE_V6", defaultSourceV6())
	geoIPCacheTTL = envDuration("GEOIP_CACHE_TTL", 24*time.Hour)
)

func defaultCacheDir() string { return "/tmp/peer-wan-geoip" }
func defaultSourceV4() string {
	return "https://raw.githubusercontent.com/ipverse/rir-ip/master/country/ipv4/%s.cidr"
}
func defaultSourceV6() string {
	return "https://raw.githubusercontent.com/ipverse/rir-ip/master/country/ipv6/%s.cidr"
}

// Defaults (exported) for external initialization.
func DefaultCacheDir() string { return defaultCacheDir() }
func DefaultSourceV4() string { return defaultSourceV4() }
func DefaultSourceV6() string { return defaultSourceV6() }

// SetConfig overrides GeoIP sources/cache based on controller settings.
func SetConfig(cfg model.GeoIPConfig) {
	if cfg.CacheDir != "" {
		geoIPCacheDir = cfg.CacheDir
	}
	if cfg.SourceV4 != "" {
		geoIPv4Source = cfg.SourceV4
	}
	if cfg.SourceV6 != "" {
		geoIPv6Source = cfg.SourceV6
	}
	if cfg.CacheTTL != "" {
		if d, err := time.ParseDuration(cfg.CacheTTL); err == nil {
			geoIPCacheTTL = d
		}
	}
}

// Expand returns normalized prefixes (/32 for IPv4) for a rule, resolving domains and geoip:CC/geoip6:CC.
func Expand(pr model.PolicyRule) []string {
	if !pr.Validate() {
		return nil
	}
	out := []string{}
	seen := map[string]struct{}{}
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	if pr.Prefix != "" {
		lower := strings.ToLower(pr.Prefix)
		switch {
		case strings.HasPrefix(lower, "geoip6:"):
			for _, p := range geoipPrefixes(strings.TrimPrefix(lower, "geoip6:"), true) {
				add(p)
			}
		case strings.HasPrefix(lower, "geoip:"):
			for _, p := range geoipPrefixes(strings.TrimPrefix(lower, "geoip:"), false) {
				add(p)
			}
		default:
			pfx := pr.Prefix
			if !strings.Contains(pfx, "/") {
				pfx = pr.Prefix + "/32"
			}
			if _, _, err := net.ParseCIDR(pfx); err == nil {
				add(pfx)
			} else if ip := net.ParseIP(pr.Prefix); ip != nil { // bare IP without mask
				add(ip.String() + "/32")
			}
		}
	}

	for _, d := range pr.Domains {
		for _, ip := range resolveDomain(d) {
			add(ip + "/32")
		}
	}
	return out
}

func geoipPrefixes(cc string, ipv6 bool) []string {
	cc = strings.TrimSpace(cc)
	if cc == "" {
		return nil
	}
	// try lowercase first to match ipverse repo layout; fallback to uppercase for custom sources
	codes := []string{strings.ToLower(cc)}
	if up := strings.ToUpper(cc); up != codes[0] {
		codes = append(codes, up)
	}
	kind := "v4"
	tmpl := geoIPv4Source
	if ipv6 {
		kind = "v6"
		tmpl = geoIPv6Source
	}
	if err := os.MkdirAll(geoIPCacheDir, 0o755); err != nil {
		return nil
	}
	for _, code := range codes {
		cacheFile := filepath.Join(geoIPCacheDir, fmt.Sprintf("%s-%s.cidr", kind, code))
		if fresh(cacheFile, geoIPCacheTTL) {
			if data, err := os.ReadFile(cacheFile); err == nil {
				return parseCIDRs(string(data))
			}
		}
		url := fmt.Sprintf(tmpl, code)
		body, err := httpGet(url)
		if err != nil {
			continue
		}
		_ = os.WriteFile(cacheFile, []byte(body), 0o644)
		return parseCIDRs(body)
	}
	return nil
}

func parseCIDRs(content string) []string {
	out := []string{}
	sc := bufio.NewScanner(strings.NewReader(content))
	seen := map[string]struct{}{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "/") {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	return out
}

func resolveDomain(domain string) []string {
	out := []string{}
	ipList, err := net.LookupIP(domain)
	if err != nil {
		return out
	}
	for _, ip := range ipList {
		if v4 := ip.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	return out
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func fresh(path string, ttl time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < ttl
}

func httpGet(url string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("status %s", resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
