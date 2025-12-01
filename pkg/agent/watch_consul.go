//go:build consul

package agent

import (
	"context"
	"strconv"
	"time"

	consulapi "github.com/hashicorp/consul/api"
)

// WatchEnabled returns true when consul tag is on.
func WatchEnabled() bool { return true }

// StartConsulWatch listens on the global plan version key and triggers onChange when it changes.
func StartConsulWatch(ctx context.Context, addr string, token string, onChange func(version int64)) error {
	cfg := consulapi.DefaultConfig()
	if addr != "" {
		cfg.Address = addr
	}
	if token != "" {
		cfg.Token = token
	}
	cli, err := consulapi.NewClient(cfg)
	if err != nil {
		return err
	}
	go func() {
		q := &consulapi.QueryOptions{}
		key := "peer-wan/plan/version"
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			kv, meta, err := cli.KV().Get(key, q)
			if err == nil && kv != nil {
				if v, parseErr := strconv.ParseInt(string(kv.Value), 10, 64); parseErr == nil {
					onChange(v)
				}
				q.WaitIndex = meta.LastIndex
			} else {
				time.Sleep(time.Second)
			}
		}
	}()
	return nil
}
