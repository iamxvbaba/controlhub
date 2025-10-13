package controlhub

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Server 维护连接与事件分发
type Server struct {
	conns    map[string]*Conn
	mu       sync.RWMutex
	auth     Authenticator
	handlers map[string]func(*Conn, []byte)
	opts     Options
	groups   map[string]map[string]*Conn // group -> clientID -> Conn

	// graceful shutdown
	httpSrv     *http.Server
	cleanupTick *time.Ticker

	onConnect    func(*Conn)
	onDisconnect func(*Conn)
}

// NewServer 创建 Server
func NewServer(auth Authenticator) *Server { return NewServerWithOptions(auth, nil) }

// NewServerWithOptions 创建 Server（支持 Options）
func NewServerWithOptions(auth Authenticator, opts *Options) *Server {
	o := DefaultOptions()
	if opts != nil {
		// 合并：仅非零值覆盖
		if opts.HeartbeatInterval != 0 {
			o.HeartbeatInterval = opts.HeartbeatInterval
		}
		if opts.HeartbeatEnabled {
			o.HeartbeatEnabled = true
		} else {
			o.HeartbeatEnabled = false
		}
		if opts.ReconnectEnabled {
			o.ReconnectEnabled = true
		} else {
			o.ReconnectEnabled = false
		}
		if opts.ReconnectBackoff != 0 {
			o.ReconnectBackoff = opts.ReconnectBackoff
		}
		if opts.ReconnectMaxBackoff != 0 {
			o.ReconnectMaxBackoff = opts.ReconnectMaxBackoff
		}
		if opts.ZombieCleanupEnabled {
			o.ZombieCleanupEnabled = true
		}
		if opts.ZombieCheckInterval != 0 {
			o.ZombieCheckInterval = opts.ZombieCheckInterval
		}
		if opts.ZombieMaxIdle != 0 {
			o.ZombieMaxIdle = opts.ZombieMaxIdle
		}
	}
	return &Server{
		conns:    make(map[string]*Conn),
		auth:     auth,
		handlers: make(map[string]func(*Conn, []byte)),
		opts:     o,
		groups:   make(map[string]map[string]*Conn),
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Serve 启动 HTTP 服务并提供 /ws 端点
func (s *Server) Serve(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)

	// 构造 http.Server 以便优雅关停
	s.httpSrv = &http.Server{Addr: addr, Handler: mux}

	// 启动僵尸连接清理
	if s.opts.ZombieCleanupEnabled && s.opts.ZombieCheckInterval > 0 && s.opts.ZombieMaxIdle > 0 {
		s.cleanupTick = time.NewTicker(s.opts.ZombieCheckInterval)
		go func() {
			for range s.cleanupTick.C {
				s.cleanupZombies()
			}
		}()
	}
	return s.httpSrv.ListenAndServe()
}

// Shutdown 优雅关闭 Server：停止 HTTP、停止清理、关闭所有连接
func (s *Server) Shutdown(ctx context.Context) error {
	if s.cleanupTick != nil {
		s.cleanupTick.Stop()
		s.cleanupTick = nil
	}
	// 先禁止新连接，然后关闭 HTTP
	if s.httpSrv != nil {
		_ = s.httpSrv.Shutdown(ctx)
	}
	// 关闭所有连接
	s.mu.RLock()
	ids := make([]string, 0, len(s.conns))
	for id := range s.conns {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		s.mu.RLock()
		c := s.conns[id]
		s.mu.RUnlock()
		if c != nil {
			_ = c.Close()
		}
		s.removeConn(id)
	}
	return nil
}

// On 注册事件处理器
func (s *Server) On(event string, handler func(c *Conn, data []byte)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[event] = handler
}

// EmitTo 向特定客户端发送事件
func (s *Server) EmitTo(clientID string, event string, payload any) error {
	s.mu.RLock()
	conn, ok := s.conns[clientID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrClientNotFound, clientID)
	}
	return conn.Emit(event, payload)
}

// Broadcast 广播事件到所有客户端
func (s *Server) Broadcast(event string, payload any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, conn := range s.conns {
		_ = conn.Emit(event, payload)
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, clientID, err := s.auth.Authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r = r.WithContext(ctx)

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}

	conn := NewConn(clientID, ws)
	s.addConn(conn)

	if s.onConnect != nil {
		// 独立 goroutine 触发，避免阻塞握手
		go s.onConnect(conn)
	}

	// Server 侧心跳（可选）。默认不启用；若启用，则发送 ping 并设置 read deadline
	if s.opts.HeartbeatEnabled && s.opts.HeartbeatInterval > 0 {
		conn.StartHeartbeat(s.opts.HeartbeatInterval)
		_ = conn.ws.SetReadDeadline(time.Now().Add(s.opts.HeartbeatInterval * 3))
	}

	go func() {
		<-conn.Closed()
		s.removeConn(conn.ID)
		if s.onDisconnect != nil {
			s.onDisconnect(conn)
		}
	}()

	go conn.Run(func(msg Message) { s.dispatch(conn, msg) })
}

func (s *Server) addConn(c *Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[c.ID] = c
}

func (s *Server) removeConn(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, id)
	// 从所有分组移除
	for g := range s.groups {
		delete(s.groups[g], id)
		if len(s.groups[g]) == 0 {
			delete(s.groups, g)
		}
	}
}

func (s *Server) dispatch(c *Conn, msg Message) {
	s.mu.RLock()
	handler, ok := s.handlers[msg.Event]
	s.mu.RUnlock()
	if ok {
		go handler(c, msg.Payload)
		return
	}
	log.Printf("[Server] unhandled event: %s", msg.Event)
}

// OnConnect 注册连接成功钩子
func (s *Server) OnConnect(h func(*Conn)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onConnect = h
}

// OnDisconnect 注册连接断开钩子
func (s *Server) OnDisconnect(h func(*Conn)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDisconnect = h
}

// cleanupZombies 关闭超时无活动的连接
func (s *Server) cleanupZombies() {
	now := time.Now()
	s.mu.RLock()
	var toClose []string
	for id, c := range s.conns {
		if c == nil {
			toClose = append(toClose, id)
			continue
		}
		last := c.LastActivity()
		if last.IsZero() {
			continue
		}
		if now.Sub(last) > s.opts.ZombieMaxIdle {
			toClose = append(toClose, id)
		}
	}
	s.mu.RUnlock()

	for _, id := range toClose {
		s.mu.RLock()
		c := s.conns[id]
		s.mu.RUnlock()
		if c != nil {
			_ = c.Close()
		}
		s.removeConn(id)
		log.Printf("[Server] cleaned zombie connection: %s", id)
	}
}

// AddToGroup 将客户端加入分组
func (s *Server) AddToGroup(group string, clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conn, ok := s.conns[clientID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrClientNotFound, clientID)
	}
	if _, ok := s.groups[group]; !ok {
		s.groups[group] = make(map[string]*Conn)
	}
	s.groups[group][clientID] = conn
	return nil
}

// RemoveFromGroup 将客户端从分组移除
func (s *Server) RemoveFromGroup(group string, clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.groups[group]; ok {
		delete(m, clientID)
		if len(m) == 0 {
			delete(s.groups, group)
		}
	}
}

// BroadcastGroup 向指定分组广播
func (s *Server) BroadcastGroup(group string, event string, payload any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m, ok := s.groups[group]; ok {
		for _, c := range m {
			_ = c.Emit(event, payload)
		}
	}
}
