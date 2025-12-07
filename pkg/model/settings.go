package model

// GeoIPConfig controls GeoIP source/caching.
type GeoIPConfig struct {
	SourceV4 string `json:"sourceV4"`
	SourceV6 string `json:"sourceV6"`
	CacheDir string `json:"cacheDir"`
	CacheTTL string `json:"cacheTtl"`
}

// DiagConfig controls diagnostics between agents.
type DiagConfig struct {
	PingInterval string `json:"pingInterval"` // e.g., "3s"
}

// Settings is a bag for global controller settings.
type Settings struct {
	GeoIP GeoIPConfig `json:"geoip"`
	Diag  DiagConfig  `json:"diag"`
}
