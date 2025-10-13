package controlhub

import "encoding/json"

// Message 表示通过 WebSocket 传递的基础消息结构
// Event 为事件名，Payload 为事件负载（原始 JSON），FromID/ToID 可选用于标识来源和目标
type Message struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
	FromID  string          `json:"from_id,omitempty"`
	ToID    string          `json:"to_id,omitempty"`
}


