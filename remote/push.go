package remote

import (
	"context"
	"errors"

	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
	"github.com/yola1107/panda/session"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/registry"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
)

// GateService 是内部 Gate 实例的注册服务名，实例 ID 对应会话中的 GateID。
const GateService = "panda.gate"

// Pusher 根据目标 GateID 直达持有会话的 Gate。
type Pusher struct {
	discovery registry.Discovery
	options   []kgrpc.ClientOption
}

// NewPusher 校验并保存服务发现依赖，选项可配置内部 RPC 的 TLS 和中间件。
func NewPusher(discovery registry.Discovery, options ...kgrpc.ClientOption) (*Pusher, error) {
	if discovery == nil {
		return nil, errors.New("gate discovery is required")
	}
	return &Pusher{discovery: discovery, options: append([]kgrpc.ClientOption(nil), options...)}, nil
}

// Push 只调用一次目标 Gate；关闭会话和队列满错误直接返回业务方。
func (p *Pusher) Push(ctx context.Context, target session.Session, route uint32, payload []byte) error {
	if p == nil || p.discovery == nil {
		return errors.New("gate discovery is required")
	}
	instances, err := p.discovery.GetService(ctx, GateService)
	if err != nil {
		return err
	}

	for _, instance := range instances {
		if instance == nil || instance.ID != target.Connection.GateID {
			continue
		}

		address := endpoint(instance)
		if address == "" {
			continue
		}

		conn, err := dial(ctx, address, p.options)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()

		_, err = internalv1.NewGateClient(conn).Deliver(ctx, &internalv1.PushRequest{
			GateId:    target.Connection.GateID,
			SessionId: target.ID,
			Route:     route,
			Payload:   payload,
		})

		return err
	}

	return kerrors.NotFound("NO_GATE", "target Gate not found")
}
