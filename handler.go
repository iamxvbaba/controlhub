package controlhub

// Handler 类型别名，用于 Server 注册事件
type Handler func(c *Conn, data []byte)
