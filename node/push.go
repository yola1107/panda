package node

import (
	"context"

	"github.com/yola1107/panda/codec"
	"github.com/yola1107/panda/protocol"
	"github.com/yola1107/panda/session"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"google.golang.org/protobuf/proto"
)

// Pusher 将已编码消息投递到目标会话持有的 Gate。
type Pusher interface {
	Push(context.Context, session.Session, uint32, []byte) error
}

// SetPusher 显式注入本地 Gate 或 RPC 推送适配器。
func (n *Node) SetPusher(pusher Pusher) {
	n.mu.Lock()
	n.pusher = pusher
	n.mu.Unlock()
}

// Push 按目标会话的 Codec 编码，失败不自动重试。
func (n *Node) Push(ctx context.Context, target session.Session, number uint32, message proto.Message) error {
	n.mu.RLock()
	s, ok := n.services[target.Connection.Service]
	pusher := n.pusher
	n.mu.RUnlock()

	if !ok || !s.codecs[target.Connection.Codec] {
		return kerrors.BadRequest("UNSUPPORTED_CODEC", "target service or codec unsupported")
	}

	if route, ok := s.routes[number]; !ok || route.Type != protocol.Push {
		return kerrors.NotFound("UNKNOWN_ROUTE", "push route not registered")
	}

	if target.ID == "" || target.UserID == "" || target.Connection.GateID == "" {
		return kerrors.BadRequest("INVALID_TARGET", "session required")
	}

	if pusher == nil {
		return kerrors.ServiceUnavailable("NO_GATE", "no push transport configured")
	}

	encoding, err := codec.Lookup(target.Connection.Codec)
	if err != nil {
		return err
	}

	payload, err := encoding.Marshal(message)
	if err != nil {
		return err
	}

	return pusher.Push(ctx, target, number, payload)
}
