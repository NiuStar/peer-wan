//go:build consul

package store

import (
	"peer-wan/pkg/consul"
)

// NewConsulStore creates a Consul-backed store (requires build tag consul).
func NewConsulStore(addr string) NodeStore {
	return consul.NewStore(addr)
}
