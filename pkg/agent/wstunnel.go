package agent

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"peer-wan/pkg/model"
	"strconv"
	"strings"
	"sync"
	"time"
)

// wstunnelManager manages wstunnel server/client processes.
type wstunnelManager struct {
	mu      sync.Mutex
	server  *exec.Cmd
	clients map[string]*exec.Cmd // key: host
	logDir  string
	binPath string
	hostMap map[string]int
	stopCh  chan struct{}
	port    int
}

var wsTunMgr = &wstunnelManager{
	clients: map[string]*exec.Cmd{},
	logDir:  "/opt/peerwan/logs",
	binPath: "/opt/peerwan/bin/wstunnel",
}

func (m *wstunnelManager) startAll(hostToLocal map[string]int, listenPort int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !fileExists(m.binPath) {
		log.Printf("wstunnel not found at %s, skip starting tunnels", m.binPath)
		return
	}
	_ = os.MkdirAll(m.logDir, 0o755)
	m.stopAllLocked()
	m.hostMap = hostToLocal
	m.port = listenPort
	m.startServerLocked(listenPort)
	for host, lp := range hostToLocal {
		m.startClientLocked(host, lp, listenPort)
	}
	m.startWatcherLocked()
}

func (m *wstunnelManager) stopAllLocked() {
	if m.stopCh != nil {
		close(m.stopCh)
		m.stopCh = nil
	}
	if m.server != nil && m.server.Process != nil {
		_ = m.server.Process.Kill()
	}
	for _, c := range m.clients {
		if c != nil && c.Process != nil {
			_ = c.Process.Kill()
		}
	}
	m.clients = map[string]*exec.Cmd{}
	m.server = nil
}

func (m *wstunnelManager) startServerLocked(port int) {
	logFile := filepath.Join(m.logDir, "wstunnel-server.log")
	// wstunnel v10+: server <ws://addr>
	args := []string{
		"server",
		fmt.Sprintf("ws://0.0.0.0:%d", port),
		"--websocket-mask-frame", // avoid plain frames on ws
	}
	cmd := exec.Command(m.binPath, args...)
	f, _ := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		log.Printf("wstunnel server start failed: %v", err)
		return
	}
	m.server = cmd
	log.Printf("wstunnel server started on ws://0.0.0.0:%d", port)
}

func (m *wstunnelManager) startClientLocked(host string, localPort, remotePort int) {
	logFile := filepath.Join(m.logDir, fmt.Sprintf("wstunnel-%s.log", host))
	local := fmt.Sprintf("udp://127.0.0.1:%d:127.0.0.1:%d", localPort, remotePort)
	remote := fmt.Sprintf("wss://%s:%d", host, remotePort)
	args := []string{
		"client",
		"--websocket-mask-frame", // mask frames to avoid proxy issues
		"-L", local,
		remote,
	}
	cmd := exec.Command(m.binPath, args...)
	f, _ := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		log.Printf("wstunnel client start failed host=%s: %v", host, err)
		return
	}
	m.clients[host] = cmd
	log.Printf("wstunnel client started host=%s local=%d remote=%d", host, localPort, remotePort)
}

func (m *wstunnelManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopAllLocked()
}

func (m *wstunnelManager) startWatcherLocked() {
	if len(m.hostMap) == 0 || m.port == 0 {
		return
	}
	ch := make(chan struct{})
	m.stopCh = ch
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				m.ensureAlive()
			case <-ch:
				return
			}
		}
	}()
}

// ensureAlive restarts missing/terminated wstunnel processes.
func (m *wstunnelManager) ensureAlive() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !fileExists(m.binPath) || m.port == 0 {
		return
	}
	if m.server == nil || (m.server.ProcessState != nil && m.server.ProcessState.Exited()) {
		m.startServerLocked(m.port)
	}
	for host, lp := range m.hostMap {
		cmd := m.clients[host]
		if cmd == nil || cmd.Process == nil || (cmd.ProcessState != nil && cmd.ProcessState.Exited()) {
			m.startClientLocked(host, lp, m.port)
		}
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// allocateLocalPorts assigns deterministic local ports for peers.
func allocateLocalPorts(peers []model.Peer, base int) map[string]int {
	hostToPort := map[string]int{}
	cur := base
	for _, p := range peers {
		host := endpointHost(p.Endpoint)
		if host == "" {
			continue
		}
		if _, ok := hostToPort[host]; ok {
			continue
		}
		hostToPort[host] = cur
		cur++
	}
	return hostToPort
}

func endpointHost(ep string) string {
	if ep == "" {
		return ""
	}
	if strings.Contains(ep, ":") {
		h, _, err := net.SplitHostPort(ep)
		if err == nil {
			return h
		}
	}
	// maybe no port
	if _, err := strconv.Atoi(ep); err == nil {
		return ""
	}
	return ep
}
