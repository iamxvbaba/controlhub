package controlhub

import "time"

// Options 控制心跳、自动重连等行为
type Options struct {
	// 心跳开关与周期
	HeartbeatEnabled  bool
	HeartbeatInterval time.Duration

	// 自动重连
	ReconnectEnabled    bool
	ReconnectBackoff    time.Duration
	ReconnectMaxBackoff time.Duration

	// 僵尸连接清理
	ZombieCleanupEnabled bool
	ZombieCheckInterval  time.Duration
	ZombieMaxIdle        time.Duration
}

// DefaultOptions 返回默认配置
func DefaultOptions() Options {
	return Options{
		HeartbeatEnabled:     true,
		HeartbeatInterval:    30 * time.Second,
		ReconnectEnabled:     false,
		ReconnectBackoff:     1 * time.Second,
		ReconnectMaxBackoff:  30 * time.Second,
		ZombieCleanupEnabled: false,
		ZombieCheckInterval:  30 * time.Second,
		ZombieMaxIdle:        2 * time.Minute,
	}
}
