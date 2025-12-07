package api

import (
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// WSMessage defines a simple envelope for agent<->controller messages.
type WSMessage struct {
	Type    string      `json:"type"`              // e.g. install_status, diag_result, command
	NodeID  string      `json:"nodeId,omitempty"`  // source/target node
	Payload interface{} `json:"payload,omitempty"` // arbitrary JSON
}

// WSHub maintains agent connections keyed by node ID.
type WSHub struct {
	upgrader websocket.Upgrader
	mu       sync.RWMutex
	agents   map[string]*websocket.Conn
	logSubs  map[string]map[*websocket.Conn]struct{} // nodeID -> set of subscribers (UI)
	taskUpd  map[string][]WSMessage                  // buffered task updates per taskId
}

func NewWSHub() *WSHub {
	return &WSHub{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		agents:  map[string]*websocket.Conn{},
		logSubs: map[string]map[*websocket.Conn]struct{}{},
		taskUpd: map[string][]WSMessage{},
	}
}

// HandleAgentWS upgrades and stores the connection for a node; expects ?nodeId=xxx
func (h *WSHub) HandleAgentWS(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeId")
	if nodeID == "" {
		http.Error(w, "nodeId required", http.StatusBadRequest)
		return
	}
	c, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade failed node=%s err=%v headers=%v", nodeID, err, r.Header)
		return
	}
	h.mu.Lock()
	if old, ok := h.agents[nodeID]; ok {
		_ = old.Close()
	}
	h.agents[nodeID] = c
	h.mu.Unlock()
	log.Printf("agent ws connected: %s", nodeID)
	go h.readLoop(nodeID, c)
}

// Send sends a message to a node if connected.
func (h *WSHub) Send(nodeID string, msg WSMessage) {
	h.mu.RLock()
	c := h.agents[nodeID]
	h.mu.RUnlock()
	if c == nil {
		log.Printf("ws send skipped; node %s not connected", nodeID)
		return
	}
	if err := c.WriteJSON(msg); err != nil {
		log.Printf("ws send to %s failed: %v", nodeID, err)
	} else {
		log.Printf("ws send to %s type=%s", nodeID, msg.Type)
	}
}

func (h *WSHub) readLoop(nodeID string, c *websocket.Conn) {
	defer func() {
		c.Close()
		h.mu.Lock()
		delete(h.agents, nodeID)
		h.mu.Unlock()
		log.Printf("agent ws disconnected: %s", nodeID)
	}()
	for {
		var msg WSMessage
		if err := c.ReadJSON(&msg); err != nil {
			return
		}
		// Currently just log; existing HTTP endpoints still persist statuses/diagnostics.
		log.Printf("ws recv from %s type=%s", nodeID, msg.Type)
		if msg.Type == "agent_log" {
			h.fanoutLogs(nodeID, msg.Payload)
		} else if msg.Type == "task_step" {
			h.handleTaskStep(msg, nodeID)
		}
	}
}

// HandleUILogs allows UI to subscribe to agent logs via WS.
func (h *WSHub) HandleUILogs(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeId")
	if nodeID == "" {
		http.Error(w, "nodeId required", http.StatusBadRequest)
		return
	}
	c, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.mu.Lock()
	if h.logSubs[nodeID] == nil {
		h.logSubs[nodeID] = map[*websocket.Conn]struct{}{}
	}
	h.logSubs[nodeID][c] = struct{}{}
	h.mu.Unlock()
	log.Printf("ui log subscriber connected node=%s", nodeID)
	go h.logSubLoop(nodeID, c)
}

func (h *WSHub) fanoutLogs(nodeID string, payload interface{}) {
	h.mu.RLock()
	subs := h.logSubs[nodeID]
	h.mu.RUnlock()
	if len(subs) == 0 {
		return
	}
	for c := range subs {
		if err := c.WriteJSON(payload); err != nil {
			go h.closeSub(nodeID, c)
		}
	}
}

// handleTaskStep stores/forwards task step updates; kept minimal (no persistence here).
func (h *WSHub) handleTaskStep(msg WSMessage, nodeID string) {
	payload, ok := msg.Payload.(map[string]interface{})
	if !ok {
		return
	}
	// buffer by taskId for potential future UI subscriptions
	taskID, _ := payload["taskId"].(string)
	if taskID == "" {
		return
	}
	h.mu.Lock()
	h.taskUpd[taskID] = append(h.taskUpd[taskID], msg)
	h.mu.Unlock()
}

func (h *WSHub) logSubLoop(nodeID string, c *websocket.Conn) {
	defer h.closeSub(nodeID, c)
	for {
		if _, _, err := c.NextReader(); err != nil {
			return
		}
	}
}

func (h *WSHub) closeSub(nodeID string, c *websocket.Conn) {
	_ = c.Close()
	h.mu.Lock()
	if subs, ok := h.logSubs[nodeID]; ok {
		delete(subs, c)
		if len(subs) == 0 {
			delete(h.logSubs, nodeID)
		}
	}
	h.mu.Unlock()
	log.Printf("ui log subscriber disconnected node=%s", nodeID)
}
