package node

import (
	"context"
	"fmt"
	"strconv"

	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
	"github.com/yola1107/panda/codec"
	"github.com/yola1107/panda/internal/fault"
	"github.com/yola1107/panda/internal/observe"
	"github.com/yola1107/panda/protocol"
	"github.com/yola1107/panda/session"

	"github.com/go-kratos/kratos/v3/encoding"
	kerrors "github.com/go-kratos/kratos/v3/errors"
	"google.golang.org/protobuf/proto"
)

func (n *Node) resolve(req *internalv1.ForwardRequest) (Route, error) {
	if err := validateConnection(req); err != nil {
		return Route{}, err
	}

	c := req.Connection

	n.mu.Lock()
	n.started = true
	s, ok := n.services[c.Service]
	n.mu.Unlock()

	if !ok {
		return Route{}, kerrors.NotFound("UNKNOWN_SERVICE", "logical service not registered")
	}
	if !s.codecs[c.Codec] {
		return Route{}, kerrors.BadRequest("UNSUPPORTED_CODEC", "codec not supported by logical service")
	}

	route, ok := s.routes[req.Route]
	if !ok || uint32(route.Type) != req.Type || route.Type == protocol.Push {
		return Route{}, kerrors.NotFound("UNKNOWN_ROUTE", "route or type not registered")
	}

	if err := validateSession(route, req.Session); err != nil {
		return Route{}, err
	}
	return route, nil
}

func (n *Node) execute(ctx context.Context, req *internalv1.ForwardRequest, route Route) (reply *internalv1.ForwardReply, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			reply = nil
			err = kerrors.InternalServer("HANDLER_PANIC", fmt.Sprint(recovered))
		}
	}()

	c := session.Connection{
		GateID:  req.Connection.GateId,
		ID:      req.Connection.ConnectionId,
		Service: req.Connection.Service,
		Codec:   req.Connection.Codec,
	}
	ctx = session.WithConnection(ctx, c)
	if req.Session != nil {
		ctx = session.WithSession(ctx, session.Session{Connection: c, ID: req.Session.Id, UserID: req.Session.UserId})
	}

	encoding, err := codec.Lookup(c.Codec)
	if err != nil {
		return nil, err
	}

	message := route.New()
	if err := encoding.Unmarshal(req.Payload, message); err != nil {
		return nil, kerrors.BadRequest("INVALID_PAYLOAD", "payload cannot be decoded")
	}

	handler := n.middleware(func(ctx context.Context, value any) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, fault.Normalize(err)
		}

		message, ok := value.(proto.Message)
		if !ok {
			return nil, kerrors.BadRequest("INVALID_PAYLOAD", "middleware changed message type")
		}

		return route.Handle(ctx, message)
	})

	value, err := observe.Server(ctx, "/node/"+c.Service+"/"+strconv.FormatUint(uint64(route.Number), 10), message, handler)
	if err != nil {
		return nil, fault.Normalize(err)
	}

	result, ok := value.(Result)
	if !ok {
		return nil, kerrors.InternalServer("INVALID_RESULT", "middleware changed Handler result type")
	}
	return encodeResult(encoding, route, result)
}

func encodeResult(encoding encoding.Codec, route Route, result Result) (*internalv1.ForwardReply, error) {
	if route.Login && result.UserID == "" {
		return nil, kerrors.Unauthorized("AUTHENTICATION_FAILED", "login returned no identity")
	}
	if !route.Login && result.UserID != "" {
		return nil, kerrors.InternalServer("INVALID_IDENTITY", "only login can bind identity")
	}

	if route.Type == protocol.Notify {
		return &internalv1.ForwardReply{}, nil
	}

	if result.Message == nil {
		return nil, kerrors.InternalServer("INVALID_RESULT", "request returned no message")
	}

	payload, err := encoding.Marshal(result.Message)
	if err != nil {
		return nil, err
	}
	return &internalv1.ForwardReply{Payload: payload, AuthenticatedUser: result.UserID}, nil
}

func validateConnection(req *internalv1.ForwardRequest) error {
	if req == nil || req.Connection == nil {
		return kerrors.Unauthorized("INVALID_CONNECTION", "missing connection")
	}
	c := req.Connection
	if c.GateId == "" || c.ConnectionId == "" || c.Service == "" || c.Codec == "" {
		return kerrors.Unauthorized("INVALID_CONNECTION", "incomplete connection")
	}
	return nil
}

func validateSession(route Route, s *internalv1.Session) error {
	if route.Login {
		if s != nil {
			return kerrors.Unauthorized("ALREADY_AUTHENTICATED", "login cannot have a session")
		}
		return nil
	}

	if s == nil || s.Id == "" || s.UserId == "" {
		return kerrors.Unauthorized("UNAUTHENTICATED", "session required")
	}
	return nil
}
