# controlhub

一个基于 WebSocket 的 Go 实时通信库：中心 Server 管理多个 Client，提供事件注册与分发、单播与广播、双向事件机制与可插拔认证。

## 安装

```bash
go get github.com/iamxvbaba/controlhub
```

## 快速开始

- 服务端示例：见 `example/server/main.go`
- 客户端示例：见 `example/client/main.go`

### Server（基于 id + secret 的认证）

```go
auth := &controlhub.SecretIDAuth{Secret: "my-secret"}
server := controlhub.NewServer(auth)
server.On("status.update", func(c *controlhub.Conn, data []byte) { fmt.Println(string(data)) })
go server.Serve(":8080")
```

支持选项：

```go
opts := controlhub.DefaultOptions()
opts.HeartbeatEnabled = true
opts.HeartbeatInterval = 15 * time.Second
server := controlhub.NewServerWithOptions(auth, &opts)
```

优雅关闭：

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
_ = server.Shutdown(ctx)
```

### Client（传递 id + secret；id 为空自动生成）

```go
client, _ := controlhub.Connect("ws://localhost:8080/ws", "my-secret")
client.Conn.On("command.sync", func(data []byte) { _ = client.Conn.Emit("status.update", map[string]string{"state":"done"}) })
select {}
```

带自动重连与心跳：

```go
opts := controlhub.DefaultOptions()
opts.HeartbeatEnabled = true
opts.HeartbeatInterval = 15 * time.Second
opts.ReconnectEnabled = true
opts.ReconnectBackoff = time.Second
opts.ReconnectMaxBackoff = 10 * time.Second
client, _ := controlhub.ConnectWithOptions("ws://localhost:8080/ws", "", "my-secret", &opts)
// 退出时优雅关闭
defer client.Close()
```

### 分组广播（Server）

```go
_ = server.AddToGroup("groupA", "clientA")
server.BroadcastGroup("groupA", "command.sync", map[string]string{"task":"refresh"})
server.RemoveFromGroup("groupA", "clientA")
```

## 设计

详见 `controlhub_design.md`。

## 许可证

MIT


