package controlhub

import "errors"

var (
	// ErrUnauthorized 认证失败
	ErrUnauthorized = errors.New("unauthorized")
	// ErrClientNotFound 未找到客户端连接
	ErrClientNotFound = errors.New("client not found")
	// ErrConnClosed 连接已关闭
	ErrConnClosed = errors.New("connection closed")
)
