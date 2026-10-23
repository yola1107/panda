// Package fault 统一本地调用和 RPC 的框架错误。
package fault

import (
	"context"
	"errors"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Overloaded 表示执行名额已满。
func Overloaded() error { return kerrors.ServiceUnavailable("OVERLOADED", "execution limit reached") }

// Closed 表示目标连接或会话已关闭。
func Closed() error { return kerrors.NotFound("SESSION_CLOSED", "session is closed") }

// Normalize 保留 Kratos 错误，并统一两种部署的超时与取消原因。
func Normalize(err error) *kerrors.Error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
		return kerrors.GatewayTimeout("TIMEOUT", "execution deadline exceeded")
	}

	if errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled {
		return kerrors.New(499, "CANCELED", "execution canceled")
	}

	return kerrors.FromError(err)
}
