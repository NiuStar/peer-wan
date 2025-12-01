package api

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// ServerTLSConfig builds a TLS config for mutual TLS when clientCA is provided.
func ServerTLSConfig(certFile, keyFile, clientCA string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load cert/key: %w", err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if clientCA != "" {
		caData, err := os.ReadFile(clientCA)
		if err != nil {
			return nil, fmt.Errorf("read client ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("invalid client ca")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}
