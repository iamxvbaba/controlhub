package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iamxvbaba/controlhub"
)

func main() {
	auth := &controlhub.SecretIDAuth{Secret: "my-secret"}

	opts := controlhub.DefaultOptions()
	opts.HeartbeatEnabled = true
	opts.ZombieCleanupEnabled = true
	server := controlhub.NewServerWithOptions(auth, &opts)

	// 连接/断开钩子
	server.OnConnect(func(c *controlhub.Conn) {
		fmt.Printf("[connect] %s\n", c.ID)
	})
	server.OnDisconnect(func(c *controlhub.Conn) {
		fmt.Printf("[disconnect] %s\n", c.ID)
	})

	server.On("status.update", func(c *controlhub.Conn, data []byte) {
		fmt.Printf("[%s] status: %s\n", c.ID, string(data))
	})

	go func() {
		_ = server.Serve(":8080")
	}()

	// 定期广播
	go func() {
		for {
			server.Broadcast("command.sync", map[string]string{"task": "refresh"})
			time.Sleep(10 * time.Second)
		}
	}()

	// 监听系统信号并优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
