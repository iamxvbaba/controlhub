package controlhub

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Conn 封装单个 WebSocket 连接与事件处理
type Conn struct {
	ID         string
	ws         *websocket.Conn
	handlers   map[string]func([]byte)
	mu         sync.RWMutex
	closed     chan struct{}
	closedOnce sync.Once
	writeMu    sync.Mutex
	// heartbeat
	heartbeatInterval time.Duration
	heartbeatTicker   *time.Ticker
	stopHeartbeat     chan struct{}
	lastActivity      time.Time
}

// NewConn 创建连接封装
func NewConn(id string, ws *websocket.Conn) *Conn {
	c := &Conn{
		ID:            id,
		ws:            ws,
		handlers:      make(map[string]func([]byte)),
		closed:        make(chan struct{}),
		stopHeartbeat: make(chan struct{}),
		lastActivity:  time.Now(),
	}
	// 默认 pong 处理：刷新读超时
	ws.SetPongHandler(func(appData string) error {
		if c.heartbeatInterval > 0 {
			_ = c.ws.SetReadDeadline(time.Now().Add(c.heartbeatInterval * 3))
		}
		c.mu.Lock()
		c.lastActivity = time.Now()
		c.mu.Unlock()
		return nil
	})
	return c
}

// On 注册事件处理器
func (c *Conn) On(event string, handler func(data []byte)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[event] = handler
}

// Emit 向对端发送事件
func (c *Conn) Emit(event string, payload any) error {
	if c == nil || c.ws == nil {
		return ErrConnClosed
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg := Message{Event: event, Payload: data}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteJSON(msg)
}

// Run 读取循环，收到消息后交给回调处理
func (c *Conn) Run(onMessage func(msg Message)) {
	// 如果启用心跳，设置初始读超时
	if c.heartbeatInterval > 0 {
		_ = c.ws.SetReadDeadline(time.Now().Add(c.heartbeatInterval * 3))
	}
	for {
		var msg Message
		if err := c.ws.ReadJSON(&msg); err != nil {
			log.Printf("[%s] read error: %v", c.ID, err)
			c.markClosed()
			return
		}
		c.mu.Lock()
		c.lastActivity = time.Now()
		c.mu.Unlock()
		onMessage(msg)
	}
}

// Close 主动关闭连接
func (c *Conn) Close() error {
	if c == nil || c.ws == nil {
		return nil
	}
	c.stopHeartbeatLoop()
	c.markClosed()
	_ = c.ws.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(time.Second),
	)
	return c.ws.Close()
}

// Closed 返回关闭通知通道
func (c *Conn) Closed() <-chan struct{} {
	return c.closed
}

// LastActivity 返回最近一次活动时间（读或 pong）
func (c *Conn) LastActivity() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastActivity
}

// StartHeartbeat 启动 ping 定时器
func (c *Conn) StartHeartbeat(interval time.Duration) {
	if interval <= 0 || c == nil || c.ws == nil {
		return
	}
	c.heartbeatInterval = interval
	if c.heartbeatTicker != nil {
		return
	}
	c.heartbeatTicker = time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-c.heartbeatTicker.C:
				c.writeMu.Lock()
				deadline := time.Now().Add(5 * time.Second)
				err := c.ws.WriteControl(websocket.PingMessage, []byte("ping"), deadline)
				c.writeMu.Unlock()
				if err != nil {
					log.Printf("[%s] ping error: %v", c.ID, err)
					c.markClosed()
					return
				}
			case <-c.stopHeartbeat:
				return
			case <-c.closed:
				return
			}
		}
	}()
}

func (c *Conn) stopHeartbeatLoop() {
	if c.heartbeatTicker != nil {
		c.heartbeatTicker.Stop()
		c.heartbeatTicker = nil
	}
	select {
	case <-c.stopHeartbeat:
	default:
		close(c.stopHeartbeat)
	}
}

// markClosed 安全关闭 closed 通道
func (c *Conn) markClosed() {
	c.closedOnce.Do(func() {
		c.stopHeartbeatLoop()
		close(c.closed)
	})
}
