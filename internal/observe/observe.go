// Package observe 为客户端入口和本地 Node 提供 Kratos tracing 上下文。
package observe

import (
	"context"
	"net/http"

	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
)

type carrier struct{ http.Header }

func (h carrier) Keys() []string {
	keys := make([]string, 0, len(h.Header))
	for key := range h.Header {
		keys = append(keys, key)
	}

	return keys
}

type requestTransport struct {
	operation string
	header    carrier
}

func (t requestTransport) Kind() transport.Kind            { return transport.Kind("panda") }
func (t requestTransport) Endpoint() string                { return "" }
func (t requestTransport) Operation() string               { return t.operation }
func (t requestTransport) RequestHeader() transport.Header { return t.header }
func (t requestTransport) ReplyHeader() transport.Header   { return t.header }

// Server 包装一个服务端操作，使用当前链路作为父级。
func Server(ctx context.Context, operation string, request any, next middleware.Handler) (any, error) {
	ctx = transport.NewServerContext(ctx, requestTransport{
		operation: operation,
		header:    carrier{Header: make(http.Header)},
	})
	return tracing.Server()(next)(ctx, request)
}
