package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"peer-wan/assets"
	"peer-wan/pkg/api"
	"peer-wan/pkg/db"
	"peer-wan/pkg/store"
	"peer-wan/pkg/version"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	showVersion := flag.Bool("v", false, "print version and exit")
	storeType := flag.String("store", getenv("STORE", "memory"), "store backend: memory|consul (requires build tag consul)")
	consulAddr := flag.String("consul-addr", getenv("CONSUL_HTTP_ADDR", "127.0.0.1:8500"), "consul address (when store=consul)")
	//consulAddr := flag.String("consul-addr", getenv("CONSUL_HTTP_ADDR", "8.222.155.32:8500"), "consul address (when store=consul)")
	tlsCert := flag.String("tls-cert", "", "TLS cert path (enables HTTPS if set with --tls-key)")
	tlsKey := flag.String("tls-key", "", "TLS key path (enables HTTPS if set with --tls-cert)")
	clientCA := flag.String("client-ca", "", "require and verify client certs using this CA (optional)")
	lockKey := flag.String("lock-key", "peer-wan/locks/leader", "Consul lock key for leader election")
	publicAddr := flag.String("public-addr", getenv("PUBLIC_ADDR", ""), "controller external base URL for agent bootstrap (e.g. https://ctrl.example.com:8080)")
	flag.Parse()

	if *showVersion {
		log.Printf("controller version=%s", version.BuildCN())
		return
	}

	dbConn, err := db.Init()
	if err != nil {
		log.Fatalf("failed to init db: %v", err)
	}
	api.SetDB(dbConn)

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
	wsHub := api.NewWSHub()
	wsHub.AttachStore(nodeStore)
	log.Printf("starting controller version=%s store=%s consul=%s publicAddr=%s", version.BuildCN(), *storeType, *consulAddr, *publicAddr)

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nodeStore, "", &planVersion, *publicAddr, *storeType, *consulAddr, wsHub)
	mux.HandleFunc("/api/v1/ws/agent", wsHub.HandleAgentWS)
	mux.HandleFunc("/api/v1/ws/logs", wsHub.HandleUILogs)

	uiFS, err := fs.Sub(assets.UI, "web")
	if err != nil {
		log.Fatalf("failed to load embedded ui: %v", err)
	}
	mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(uiFS))))

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
	// periodic prune health history (>24h)
	go func() {
		t := time.NewTicker(1 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if pruner, ok := nodeStore.(interface{ PruneHealthBefore(time.Time) error }); ok {
					cutoff := time.Now().Add(-24 * time.Hour)
					if err := pruner.PruneHealthBefore(cutoff); err != nil {
						log.Printf("prune health history failed: %v", err)
					}
				}
			}
		}
	}()
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

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
