package gate

import (
	"context"
	"strconv"
	"time"

	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
	"github.com/yola1107/panda/codec"
	"github.com/yola1107/panda/internal/fault"
	"github.com/yola1107/panda/internal/observe"
	"github.com/yola1107/panda/protocol"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/google/uuid"
)

func (c *connection) expireAuthentication() {
	c.mu.Lock()
	unauthenticated := c.session == nil
	c.mu.Unlock()

	if unauthenticated {
		c.close()
	}
}

func (c *connection) admit(m protocol.Message) (*internalv1.Session, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, false, fault.Closed()
	}

	login := m.Type == protocol.Request && m.Route == c.entry.LoginRoute
	if c.session == nil {
		if !login {
			return nil, false, kerrors.Unauthorized("UNAUTHENTICATED", "login required")
		}

		if c.login {
			return nil, false, fault.Overloaded()
		}

		if !time.Now().Before(c.authDeadline) {
			return nil, false, fault.Normalize(context.DeadlineExceeded)
		}
	} else if login {
		return nil, false, kerrors.BadRequest("ALREADY_AUTHENTICATED", "session already bound")
	}

	select {
	case c.slots <- struct{}{}:
	default:
		return nil, false, fault.Overloaded()
	}

	if login {
		c.login = true
	}

	return c.session, login, nil
}

func (c *connection) dispatch(ctx context.Context, m protocol.Message) {
	s, login, err := c.admit(m)
	if err != nil {
		c.reject(m, err)
		return
	}

	c.workers.Go(func() {
		defer func() {
			<-c.slots

			if login {
				c.mu.Lock()
				c.login = false
				c.mu.Unlock()
			}
		}()

		deadline := time.Now().Add(c.entry.ExecutionTimeout)
		if login && c.authDeadline.Before(deadline) {
			deadline = c.authDeadline
		}

		ctx, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()

		c.forward(ctx, m, s, login)
	})
}

func (c *connection) forward(ctx context.Context, m protocol.Message, s *internalv1.Session, login bool) {
	backend, err := c.selectNode(ctx)
	if err != nil {
		c.reject(m, err)
		return
	}

	req := &internalv1.ForwardRequest{
		Connection: &internalv1.Connection{
			GateId:       c.gate.config.ID,
			ConnectionId: c.id,
			Service:      c.entry.Service,
			Codec:        c.entry.Codec,
		},
		Session: s,
		Route:   m.Route,
		Type:    uint32(m.Type),
		Payload: m.Payload,
	}
	operation := "/gate/" + c.entry.Service + "/" + strconv.FormatUint(uint64(m.Route), 10)
	value, err := observe.Server(ctx, operation, req, func(ctx context.Context, _ any) (any, error) {
		return backend.Forward(ctx, req)
	})
	reply, _ := value.(*internalv1.ForwardReply)

	if ctx.Err() != nil {
		err = ctx.Err()
	}

	if err != nil {
		c.reject(m, err)
		return
	}

	if reply == nil {
		c.reject(m, kerrors.InternalServer("INVALID_RESULT", "Node returned no result"))
		return
	}

	if login && !c.bind(ctx, reply.AuthenticatedUser) {
		c.reject(m, kerrors.Unauthorized("AUTHENTICATION_FAILED", "login is no longer valid"))
		return
	}

	if m.Type == protocol.Request {
		c.enqueue(protocol.Message{Type: protocol.Response, Route: m.Route, RequestID: m.RequestID, Payload: reply.Payload})
	}
}

func (c *connection) selectNode(ctx context.Context) (Backend, error) {
	c.mu.Lock()
	backend := c.backend
	c.mu.Unlock()

	if backend != nil {
		return backend, nil
	}

	if c.gate.selector == nil {
		return nil, kerrors.ServiceUnavailable("NO_NODE", "no Node selector configured")
	}

	backend, err := c.gate.selector.Select(ctx, c.entry.Service)
	if err != nil {
		return nil, err
	}

	if backend == nil {
		return nil, kerrors.ServiceUnavailable("NO_NODE", "no Node available")
	}

	c.mu.Lock()
	c.backend = backend
	c.mu.Unlock()
	// 监控使用连接生命周期，由 run 负责等待。
	c.workers.Go(func() {
		select {
		case <-backend.Done():
			c.close()
		case <-c.done:
		}
	})

	return backend, nil
}

func (c *connection) bind(ctx context.Context, user string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.session != nil || !c.login || user == "" || ctx.Err() != nil || !time.Now().Before(c.authDeadline) {
		return false
	}

	c.session = &internalv1.Session{Id: uuid.NewString(), UserId: user}

	return true
}

func (c *connection) reject(m protocol.Message, err error) {
	if m.Type == protocol.Notify {
		c.close()
		return
	}

	failure := fault.Normalize(err)

	encoding, codecErr := codec.Lookup(c.entry.Codec)
	if codecErr != nil {
		c.close()
		return
	}

	payload, codecErr := encoding.Marshal(&failure.Status)
	if codecErr != nil {
		c.close()
		return
	}

	c.enqueue(protocol.Message{
		Type:      protocol.Response,
		Route:     m.Route,
		RequestID: m.RequestID,
		Code:      failure.Code,
		Payload:   payload,
	})
}
