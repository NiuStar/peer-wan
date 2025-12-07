package agent

import (
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsClient maintains a single ws connection to controller for commands/status streaming.
type wsClient struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	endpoint string
	token    string
	prov     string
	nodeID   string
	handlers map[string]func(map[string]interface{})
	logs     chan string
	stopLogs chan struct{}
}

func newWSClient(controller, nodeID, authToken, provisionToken string) *wsClient {
	if controller == "" || nodeID == "" {
		return nil
	}
	u, err := url.Parse(controller)
	if err != nil {
		return nil
	}
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	u.Scheme = scheme
	u.Path = "/api/v1/ws/agent"
	q := u.Query()
	q.Set("nodeId", nodeID)
	u.RawQuery = q.Encode()
	return &wsClient{
		endpoint: u.String(),
		token:    authToken,
		prov:     provisionToken,
		nodeID:   nodeID,
		handlers: map[string]func(map[string]interface{}){},
		logs:     make(chan string, 200),
		stopLogs: make(chan struct{}),
	}
}

func (c *wsClient) start() {
	if c == nil {
		return
	}
	go c.loop()
	go c.flushLogs()
}

func (c *wsClient) loop() {
	for {
		dialer := websocket.DefaultDialer
		header := http.Header{}
		if c.token != "" {
			header.Set("Authorization", "Bearer "+c.token)
		}
		if c.prov != "" {
			header.Set("X-Provision-Token", c.prov)
		}
		conn, resp, err := dialer.Dial(c.endpoint, header)
		if err != nil {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			log.Printf("ws dial failed: %v (url=%s status=%d)", err, c.endpoint, status)
			time.Sleep(5 * time.Second)
			continue
		}
		c.mu.Lock()
		c.conn = conn
		c.mu.Unlock()
		log.Printf("ws connected to controller url=%s", c.endpoint)
		c.readLoop(conn)
		log.Printf("ws disconnected, retrying in 5s")
		time.Sleep(5 * time.Second)
	}
}

func (c *wsClient) readLoop(conn *websocket.Conn) {
	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		t, _ := msg["type"].(string)
		payload, _ := msg["payload"].(map[string]interface{})
		log.Printf("ws recv type=%s payloadKeys=%d", t, len(payload))
		if h, ok := c.handlers[t]; ok {
			go h(payload)
		}
	}
}

func (c *wsClient) send(msg interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return
	}
	if err := c.conn.WriteJSON(msg); err != nil {
		log.Printf("ws send failed: %v", err)
	} else {
		log.Printf("ws send ok")
	}
}

// flushLogs periodically sends buffered log lines to controller (best effort).
func (c *wsClient) flushLogs() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.stopLogs:
			return
		case <-t.C:
			c.drainLogs()
		}
	}
}

func (c *wsClient) drainLogs() {
	if c == nil || c.conn == nil {
		return
	}
	lines := []string{}
Loop:
	for i := 0; i < 50; i++ { // cap batch size
		select {
		case l := <-c.logs:
			lines = append(lines, l)
		default:
			break Loop
		}
	}
	if len(lines) == 0 {
		return
	}
	payload := map[string]interface{}{
		"type":    "agent_log",
		"nodeId":  c.nodeID,
		"payload": map[string]interface{}{"lines": lines, "ts": time.Now().Unix()},
	}
	_ = c.conn.WriteJSON(payload)
}

func (c *wsClient) pushLog(line string) {
	select {
	case c.logs <- line:
	default:
	}
}

func (c *wsClient) on(msgType string, fn func(map[string]interface{})) {
	c.handlers[msgType] = fn
}
