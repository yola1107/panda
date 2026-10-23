package gate

import (
	"context"

	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
)

// Backend 是已选定的 Node，会话期间不重新选择。
type Backend interface {
	Forward(context.Context, *internalv1.ForwardRequest) (*internalv1.ForwardReply, error)
	Done() <-chan struct{}
}

// Selector 在登录前按逻辑服务选择一个 Node。
type Selector interface {
	Select(context.Context, string) (Backend, error)
}

type local struct{ backend Backend }

// Local 将本地 Node 接入与 RPC 相同的 Forward 契约。
func Local(backend Backend) Selector { return local{backend: backend} }

func (l local) Select(_ context.Context, _ string) (Backend, error) { return l.backend, nil }

// Option 配置 Gate 的运行时依赖。
type Option func(*Gate)

// WithSelector 设置登录使用的 Node 选择器。
func WithSelector(selector Selector) Option {
	return func(g *Gate) { g.selector = selector }
}
