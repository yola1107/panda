// Package main panda-example 演示组合部署、分离部署和类型化 Go 客户端。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	echov1 "github.com/yola1107/panda/api/echo/v1"
	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
	"github.com/yola1107/panda/client"
	"github.com/yola1107/panda/examples/echo"
	"github.com/yola1107/panda/gate"
	"github.com/yola1107/panda/node"
	"github.com/yola1107/panda/remote"

	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	"github.com/go-kratos/kratos/v3"
	"github.com/go-kratos/kratos/v3/transport"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
)

func main() {
	mode := flag.String("mode", "combined", "combined、gate、node 或 client")
	path := flag.String("config", "examples/echo/config.yaml", "配置文件")
	entry := flag.Int("entry", 0, "客户端选择的入口索引")
	flag.Parse()

	if err := run(*mode, *path, *entry); err != nil {
		slog.Error("示例运行失败", "error", err)
		os.Exit(1)
	}
}

func run(mode, path string, entry int) error {
	conf, err := echo.Load(path)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if mode == "client" {
		return demonstrate(ctx, conf, entry)
	}

	n, err := node.New(node.Config{MaxExecuting: conf.NodeMaxExecuting})
	if err != nil {
		return err
	}

	if err := echov1.RegisterEcho(n, &echo.Handler{Node: n}); err != nil {
		return err
	}

	var servers []transport.Server
	switch mode {
	case "combined":
		g, err := gate.New(conf.Gate, gate.WithSelector(gate.Local(n)))
		if err != nil {
			return err
		}

		n.SetPusher(g)
		servers = []transport.Server{g, n}
	case "node":
		pusher, err := remote.NewPusher(conf.Registry())
		if err != nil {
			return err
		}
		n.SetPusher(pusher)
		rpc := internalServer(conf.NodeRPC)
		internalv1.RegisterNodeServer(rpc, n)
		servers = []transport.Server{n, rpc}
	case "gate":
		nodes, err := remote.NewNodes(ctx, conf.Registry(), []string{echov1.EchoService})
		if err != nil {
			return err
		}
		defer nodes.Close()

		g, err := gate.New(conf.Gate, gate.WithSelector(nodes))
		if err != nil {
			return err
		}

		rpc := internalServer(conf.GateRPC)
		internalv1.RegisterGateServer(rpc, g)
		servers = []transport.Server{g, rpc}
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}

	return kratos.New(kratos.Name("panda-example"), kratos.Server(servers...)).Run()
}

func internalServer(address string) *kgrpc.Server {
	return kgrpc.NewServer(kgrpc.Address(address), kgrpc.Timeout(0), kgrpc.Middleware(tracing.Server()))
}

func demonstrate(ctx context.Context, conf echo.Config, index int) error {
	if index < 0 || index >= len(conf.Gate.Entries) {
		return fmt.Errorf("entry index %d out of range", index)
	}

	e := conf.Gate.Entries[index]
	address := e.Address
	if e.Network == "websocket" {
		address = "ws://" + address + e.Path
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	c, err := client.Dial(ctx, client.Config{
		Network: e.Network,
		Address: address,
		Codec:   e.Codec,
		MaxSize: e.MaxSize,
	})
	if err != nil {
		return err
	}
	defer c.Close()

	api := echov1.NewEchoPandaClient(c)
	updates := make(chan *echov1.Update, 1)
	api.OnUpdate(func(_ context.Context, message *echov1.Update) error {
		updates <- message
		return nil
	})

	login, err := api.Login(ctx, &echov1.LoginRequest{Token: "demo-token"})
	if err != nil {
		return err
	}
	slog.Info("登录成功", "greeting", login.Greeting)

	reply, err := api.Say(ctx, &echov1.SayRequest{Text: "你好，Panda"})
	if err != nil {
		return err
	}
	slog.Info("收到应答", "text", reply.Text, "user", reply.UserId)

	if err := api.Report(ctx, &echov1.ReportRequest{Text: "业务已处理通知"}); err != nil {
		return err
	}

	select {
	case update := <-updates:
		slog.Info("收到主动推送", "text", update.Text)
	case <-ctx.Done():
		return ctx.Err()
	}

	return c.Heartbeat(ctx)
}
