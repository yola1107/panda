package gate

import (
	"context"

	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
	"github.com/yola1107/panda/internal/fault"
	"github.com/yola1107/panda/protocol"
	"github.com/yola1107/panda/session"

	kerrors "github.com/go-kratos/kratos/v3/errors"
)

// Push 为本地 Node 投递按目标 Codec 编码的 payload。
func (g *Gate) Push(ctx context.Context, target session.Session, route uint32, payload []byte) error {
	_, err := g.Deliver(ctx, &internalv1.PushRequest{
		GateId:    target.Connection.GateID,
		SessionId: target.ID,
		Route:     route,
		Payload:   payload,
	})
	return err
}

// Deliver 是内部 RPC 与本地调用共用的推送入口；队列满会关闭连接。
func (g *Gate) Deliver(ctx context.Context, req *internalv1.PushRequest) (*internalv1.PushReply, error) {
	if err := ctx.Err(); err != nil {
		return nil, fault.Normalize(err)
	}

	if req == nil || req.GateId != g.config.ID || req.SessionId == "" || req.Route == 0 {
		return nil, kerrors.BadRequest("INVALID_TARGET", "invalid Gate or session")
	}

	c := g.findSession(req.SessionId)
	if c == nil {
		return nil, fault.Closed()
	}

	if !c.enqueue(protocol.Message{Type: protocol.Push, Route: req.Route, Payload: req.Payload}) {
		return nil, kerrors.ServiceUnavailable("DELIVERY_FAILED", "connection closed or send queue full")
	}

	return &internalv1.PushReply{}, nil
}

func (g *Gate) findSession(id string) *connection {
	g.mu.Lock()
	defer g.mu.Unlock()

	for c := range g.connections {
		c.mu.Lock()
		match := !c.closed && c.session != nil && c.session.Id == id
		c.mu.Unlock()

		if match {
			return c
		}
	}

	return nil
}
