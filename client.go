package controlhub

import (
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Client 封装客户端行为
type Client struct {
	Conn *Conn

	url    string
	id     string
	secret string
	opts   Options

	mu   sync.Mutex
	stop chan struct{}
}

// Connect 连接到 Server（使用默认 Options）
func Connect(urlStr string, secret string) (*Client, error) {
	return ConnectWithOptions(urlStr, "", secret, nil)
}

// ConnectWithOptions 连接到 Server，并启动读取循环（支持 Options）
// 若 id 为空，将自动随机生成；secret 必填
func ConnectWithOptions(urlStr string, id string, secret string, opts *Options) (*Client, error) {
	o := DefaultOptions()
	if opts != nil {
		if opts.HeartbeatInterval != 0 {
			o.HeartbeatInterval = opts.HeartbeatInterval
		}
		o.HeartbeatEnabled = opts.HeartbeatEnabled
		o.ReconnectEnabled = opts.ReconnectEnabled
		if opts.ReconnectBackoff != 0 {
			o.ReconnectBackoff = opts.ReconnectBackoff
		}
		if opts.ReconnectMaxBackoff != 0 {
			o.ReconnectMaxBackoff = opts.ReconnectMaxBackoff
		}
	}

	// 兼容两种传递方式：Header 与 Query（Server 支持双方式）
	header := http.Header{}
	header.Set("X-Client-Secret", secret)
	if id == "" {
		id = uuid.NewString()
	}
	header.Set("X-Client-ID", id)

	// 附带 query 以便跨代理丢头场景
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("id", id)
	q.Set("secret", secret)
	u.RawQuery = q.Encode()

	ws, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		return nil, err
	}
	conn := NewConn(id, ws)
	if o.HeartbeatEnabled && o.HeartbeatInterval > 0 {
		conn.StartHeartbeat(o.HeartbeatInterval)
		_ = conn.ws.SetReadDeadline(time.Now().Add(o.HeartbeatInterval * 3))
	}
	client := &Client{Conn: conn, url: u.String(), id: id, secret: secret, opts: o, stop: make(chan struct{})}
	go client.runReadLoop()
	if o.ReconnectEnabled {
		go client.reconnectWatcher()
	}
	return client, nil
}

func (c *Client) runReadLoop() {
	current := c.Conn
	current.Run(func(msg Message) {
		current.mu.RLock()
		handler, ok := current.handlers[msg.Event]
		current.mu.RUnlock()
		if ok {
			go handler(msg.Payload)
		}
	})
}

// Emit 代理到当前连接（便于在自动重连时避免使用旧指针）
func (c *Client) Emit(event string, payload any) error {
	c.mu.Lock()
	conn := c.Conn
	c.mu.Unlock()
	if conn == nil {
		return ErrConnClosed
	}
	return conn.Emit(event, payload)
}

func (c *Client) reconnectWatcher() {
	// 同时监听 stop 与当前连接关闭，防止 stop 已关闭仍阻塞在连接关闭等待
	select {
	case <-c.stop:
		return
	case <-c.Conn.Closed():
	}
	backoff := c.opts.ReconnectBackoff
	if backoff <= 0 {
		backoff = time.Second
	}
	for attempt := 1; ; attempt++ {
		select {
		case <-c.stop:
			return
		default:
		}

		header := http.Header{}
		header.Set("X-Client-Secret", c.secret)
		header.Set("X-Client-ID", c.id)
		ws, _, err := websocket.DefaultDialer.Dial(c.url, header)
		if err != nil {
			time.Sleep(backoff)
			backoff *= 2
			if max := c.opts.ReconnectMaxBackoff; max > 0 && backoff > max {
				backoff = max
			}
			continue
		}

		newConn := NewConn(uuid.NewString(), ws)
		if c.opts.HeartbeatEnabled && c.opts.HeartbeatInterval > 0 {
			newConn.StartHeartbeat(c.opts.HeartbeatInterval)
			_ = newConn.ws.SetReadDeadline(time.Now().Add(c.opts.HeartbeatInterval * 3))
		}

		// 复制旧 handlers
		old := c.Conn
		old.mu.RLock()
		handlers := make(map[string]func([]byte), len(old.handlers))
		for k, v := range old.handlers {
			handlers[k] = v
		}
		old.mu.RUnlock()
		newConn.mu.Lock()
		newConn.handlers = handlers
		newConn.mu.Unlock()

		// 切换连接
		c.mu.Lock()
		c.Conn = newConn
		c.mu.Unlock()

		go c.runReadLoop()
		// 继续监视新连接
		go c.reconnectWatcher()
		return
	}
}

// Close 停止自动重连并关闭当前连接
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.stop != nil {
		select {
		case <-c.stop:
		default:
			close(c.stop)
		}
	}
	conn := c.Conn
	c.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}
