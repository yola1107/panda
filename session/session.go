// Package session 提供 Handler 的可信连接和会话描述。
package session

import "context"

// Connection 由 Gate 按入口填写，登录时尚无用户身份。
type Connection struct {
	GateID  string
	ID      string
	Service string
	Codec   string
}

// Session 标识当前 Gate 持有的已认证会话；M1 不提供恢复能力。
type Session struct {
	Connection Connection
	ID         string
	UserID     string
}

type (
	connectionKey struct{}
	sessionKey    struct{}
)

// WithConnection 供 Node 在验证内部转交后建立 Handler 上下文。
func WithConnection(ctx context.Context, c Connection) context.Context {
	return context.WithValue(ctx, connectionKey{}, c)
}

// ConnectionFromContext 返回当前连接；登录 Handler 也能使用。
func ConnectionFromContext(ctx context.Context) (Connection, bool) {
	c, ok := ctx.Value(connectionKey{}).(Connection)
	return c, ok
}

// WithSession 供 Node 在验证已认证转交后建立 Handler 上下文。
func WithSession(ctx context.Context, s Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// FromContext 返回可信会话；登录 Handler 中不存在该值。
func FromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(Session)
	return s, ok
}
