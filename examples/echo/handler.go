// Package echo 提供两种部署共用的示例业务。
package echo

import (
	"context"

	echov1 "github.com/yola1107/panda/api/echo/v1"
	"github.com/yola1107/panda/node"
	"github.com/yola1107/panda/session"

	kerrors "github.com/go-kratos/kratos/v3/errors"
)

// Handler 实现生成的业务接口，示例令牌仅用于本地演示。
type Handler struct{ Node *node.Node }

// Login 校验演示令牌，身份不从客户端的用户字段获取。
func (*Handler) Login(_ context.Context, request *echov1.LoginRequest) (*echov1.LoginReply, string, error) {
	if request.Token != "demo-token" {
		return nil, "", kerrors.Unauthorized("INVALID_TOKEN", "invalid demo token")
	}

	return &echov1.LoginReply{Greeting: "欢迎使用 Panda"}, "demo-user", nil
}

// Say 从会话取得可信身份并返回请求内容。
func (*Handler) Say(ctx context.Context, request *echov1.SayRequest) (*echov1.SayReply, error) {
	s, ok := session.FromContext(ctx)
	if !ok {
		return nil, kerrors.Unauthorized("UNAUTHENTICATED", "session required")
	}

	if request.Text == "error" {
		return nil, kerrors.BadRequest("DEMO_ERROR", "示例业务错误").WithMetadata(map[string]string{"field": "text"})
	}

	return &echov1.SayReply{Text: request.Text, UserId: s.UserID}, nil
}

// Report 接收通知，业务完成后通过生成代码按会话主动推送。
func (h *Handler) Report(ctx context.Context, request *echov1.ReportRequest) error {
	s, ok := session.FromContext(ctx)
	if !ok {
		return kerrors.Unauthorized("UNAUTHENTICATED", "session required")
	}

	return echov1.PushEchoUpdate(ctx, h.Node, s, &echov1.Update{Text: request.Text})
}
