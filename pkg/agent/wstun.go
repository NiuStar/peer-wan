package agent

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type wssTunnel struct {
	listenPort int
	wssPort    int
	targets    []string
	udpConn    net.PacketConn
	udpInject  net.Conn
	server     *http.Server
	clientsMu  sync.RWMutex
	clients    map[*websocket.Conn]struct{}
	stop       chan struct{}
}

var tunnelMu sync.Mutex
var tunnel *wssTunnel

// configureWSTunnel starts/restarts UDP<->WSS tunnel (naive broadcast) using given targets.
// This is a minimal, unverified implementation intended for basic traversal.
func configureWSTunnel(targetHosts []string, listenPort, wssPort int) {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()
	changed := false
	if tunnel == nil {
		changed = true
	} else if tunnel.listenPort != listenPort || tunnel.wssPort != wssPort || !equalStrings(tunnel.targets, targetHosts) {
		changed = true
		tunnel.stopTunnel()
		tunnel = nil
	}
	if !changed {
		return
	}
	tun := &wssTunnel{
		listenPort: listenPort,
		wssPort:    wssPort,
		targets:    targetHosts,
		clients:    map[*websocket.Conn]struct{}{},
		stop:       make(chan struct{}),
	}
	if err := tun.start(); err != nil {
		log.Printf("wss tunnel start failed: %v", err)
		return
	}
	tunnel = tun
}

func (t *wssTunnel) start() error {
	var err error
	t.udpConn, err = net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", t.listenPort))
	if err != nil {
		return err
	}
	t.udpInject, err = net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", t.listenPort))
	if err != nil {
		return err
	}
	go t.udpReader()
	go t.startServer()
	for _, host := range t.targets {
		go t.startClient(host)
	}
	return nil
}

func (t *wssTunnel) stopTunnel() {
	close(t.stop)
	if t.server != nil {
		_ = t.server.Close()
	}
	if t.udpConn != nil {
		_ = t.udpConn.Close()
	}
	if t.udpInject != nil {
		_ = t.udpInject.Close()
	}
	t.clientsMu.Lock()
	for c := range t.clients {
		_ = c.Close()
	}
	t.clients = map[*websocket.Conn]struct{}{}
	t.clientsMu.Unlock()
}

func (t *wssTunnel) udpReader() {
	buf := make([]byte, 65535)
	for {
		t.udpConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, _, err := t.udpConn.ReadFrom(buf)
		if err != nil {
			select {
			case <-t.stop:
				return
			default:
			}
			continue
		}
		t.clientsMu.RLock()
		for c := range t.clients {
			_ = c.WriteMessage(websocket.BinaryMessage, buf[:n])
		}
		t.clientsMu.RUnlock()
	}
}

func (t *wssTunnel) startServer() {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		t.registerConn(conn)
	})
	t.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", t.wssPort),
		Handler: mux,
	}
	go func() {
		<-t.stop
		_ = t.server.Close()
	}()
	if err := t.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Printf("wss server error: %v", err)
	}
}

func (t *wssTunnel) startClient(host string) {
	url := fmt.Sprintf("wss://%s:%d/", host, t.wssPort)
	for {
		select {
		case <-t.stop:
			return
		default:
		}
		dialer := websocket.Dialer{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // skip cert verify as requested
		}
		conn, _, err := dialer.Dial(url, nil)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}
		t.registerConn(conn)
		t.readLoop(conn)
		time.Sleep(5 * time.Second)
	}
}

func (t *wssTunnel) registerConn(c *websocket.Conn) {
	t.clientsMu.Lock()
	t.clients[c] = struct{}{}
	t.clientsMu.Unlock()
	go func() {
		t.readLoop(c)
	}()
}

func (t *wssTunnel) readLoop(c *websocket.Conn) {
	defer func() {
		t.clientsMu.Lock()
		delete(t.clients, c)
		t.clientsMu.Unlock()
		_ = c.Close()
	}()
	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			return
		}
		_, _ = t.udpInject.Write(data)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// deriveHosts extracts host portion from endpoints host:port.
func deriveHosts(peers []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, ep := range peers {
		host := ep
		if strings.Contains(host, ":") {
			host, _, _ = net.SplitHostPort(ep)
		}
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		out = append(out, host)
	}
	return out
}
