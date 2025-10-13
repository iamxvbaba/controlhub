package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iamxvbaba/controlhub"
)

func main() {
	opts := controlhub.DefaultOptions()
	opts.HeartbeatEnabled = true
	opts.HeartbeatInterval = 10 * time.Second
	opts.ReconnectEnabled = true
	opts.ReconnectBackoff = time.Second
	opts.ReconnectMaxBackoff = 10 * time.Second

	client, err := controlhub.ConnectWithOptions("ws://localhost:8080/ws", "", "my-secret", &opts)
	if err != nil {
		panic(err)
	}

	client.Conn.On("command.sync", func(data []byte) {
		fmt.Println("Received command:", string(data))
		_ = client.Conn.Emit("status.update", map[string]string{"state": "done"})
	})

	// 监听信号并优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	_ = client.Close()
}
