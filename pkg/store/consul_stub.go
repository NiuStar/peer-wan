//go:build !consul

package store

import (
	"log"
)

// NewConsulStore returns a memory store when the consul build tag is not enabled.
func NewConsulStore(addr string) NodeStore {
	log.Printf("consul store requested (addr=%s) but consul build tag not enabled; using memory store", addr)
	return NewMemoryStore()
}

// StartWatch/LeaderGuard are no-ops for non-consul builds; not implemented here to avoid duplicate methods.
