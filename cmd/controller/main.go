package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"context"

	"peer-wan/pkg/api"
	"peer-wan/pkg/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	token := flag.String("token", "", "bootstrap auth token (optional)")
	storeType := flag.String("store", "memory", "store backend: memory|consul (requires build tag consul)")
	consulAddr := flag.String("consul-addr", "127.0.0.1:8500", "consul address (when store=consul)")
	tlsCert := flag.String("tls-cert", "", "TLS cert path (enables HTTPS if set with --tls-key)")
	tlsKey := flag.String("tls-key", "", "TLS key path (enables HTTPS if set with --tls-cert)")
	clientCA := flag.String("client-ca", "", "require and verify client certs using this CA (optional)")
	lockKey := flag.String("lock-key", "peer-wan/locks/leader", "Consul lock key for leader election")
	flag.Parse()

	var planVersion int64
	var nodeStore store.NodeStore
	switch *storeType {
	case "consul":
		nodeStore = store.NewConsulStore(*consulAddr)
	case "memory":
		nodeStore = store.NewMemoryStore()
	default:
		log.Fatalf("unsupported store type: %s", *storeType)
	}

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nodeStore, *token, &planVersion)
	mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.Dir("web"))))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if w, ok := nodeStore.(interface {
		StartWatch(context.Context, func())
	}); ok && *storeType == "consul" {
		w.StartWatch(ctx, func() {
			if err := api.RecomputeAllPlans(nodeStore, &planVersion); err != nil {
				log.Printf("consul watch recompute failed: %v", err)
			}
			api.BumpPlanVersion(&planVersion)
			log.Printf("consul watch triggered; planVersion=%d", planVersion)
		})
	}
	if lg, ok := nodeStore.(interface {
		LeaderGuard(context.Context, string, time.Duration, func(context.Context))
	}); ok && *storeType == "consul" {
		go lg.LeaderGuard(ctx, *lockKey, 15*time.Second, func(lctx context.Context) {
			log.Printf("leader acquired lock %s; watching for plan changes", *lockKey)
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-lctx.Done():
					log.Printf("leader lost lock %s", *lockKey)
					return
				case <-ticker.C:
					if err := api.RecomputeAllPlans(nodeStore, &planVersion); err != nil {
						log.Printf("leader recompute failed: %v", err)
					}
				}
			}
		})
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("controller listening on %s", *addr)
	var err error
	if *tlsCert != "" && *tlsKey != "" {
		if *clientCA != "" {
			cfg, errTLS := api.ServerTLSConfig(*tlsCert, *tlsKey, *clientCA)
			if errTLS != nil {
				log.Fatalf("failed to build TLS config: %v", errTLS)
			}
			srv.TLSConfig = cfg
			err = srv.ListenAndServeTLS("", "")
		} else {
			err = srv.ListenAndServeTLS(*tlsCert, *tlsKey)
		}
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil {
		log.Fatalf("server error: %v", err)
	}
}
