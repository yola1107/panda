package echo_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	echov1 "github.com/yola1107/panda/api/echo/v1"
	"github.com/yola1107/panda/gate"
	"github.com/yola1107/panda/protocol"

	"github.com/go-kratos/kratos/v3/middleware"
)

func TestGateStopCancelsHandlers(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()
		entered, exited := make(chan struct{}, 4), make(chan struct{}, 4)
		h := start(t, mode, settings{customize: func(h *handler) {
			h.say = func(ctx context.Context, _ *echov1.SayRequest) (*echov1.SayReply, error) {
				entered <- struct{}{}
				<-ctx.Done()
				exited <- struct{}{}
				return nil, ctx.Err()
			}
		}})
		c, api := h.dial(t)
		login(t, api)
		results := make(chan error, 4)
		for range 4 {
			go func() {
				_, err := api.Say(t.Context(), &echov1.SayRequest{})
				results <- err
			}()
		}
		for range 4 {
			receive(t, entered)
		}
		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()
		if err := h.gate.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		receive(t, c.Done())
		for range 4 {
			receive(t, exited)
			if receive(t, results) == nil {
				t.Fatal("closed request succeeded")
			}
		}
	})
}

func TestDisconnectedLogin(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()
		entered, canceled, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		h := start(t, mode, settings{customize: func(h *handler) {
			h.login = func(ctx context.Context, _ *echov1.LoginRequest) (*echov1.LoginReply, string, error) {
				close(entered)
				<-ctx.Done()
				close(canceled)
				<-release
				close(exited)
				return &echov1.LoginReply{}, "late", nil
			}
		}})
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		c, api := h.dial(t)
		result := make(chan error, 1)
		go func() {
			_, err := api.Login(t.Context(), &echov1.LoginRequest{})
			result <- err
		}()
		receive(t, entered)
		c.Close()
		receive(t, canceled)
		close(release)
		receive(t, exited)
		if receive(t, result) == nil {
			t.Fatal("disconnected login succeeded")
		}
	})
}

func TestCanceledHandlerDoesNotStart(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()
		var calls atomic.Int32
		passed := make(chan struct{}, 1)
		delay := func(next middleware.Handler) middleware.Handler {
			return func(ctx context.Context, request any) (any, error) {
				if _, ok := request.(*echov1.SayRequest); !ok {
					return next(ctx, request)
				}
				<-ctx.Done()
				result, err := next(ctx, request)
				passed <- struct{}{}
				return result, err
			}
		}
		h := start(t, mode, settings{
			entry:      gate.Entry{ExecutionTimeout: 100 * time.Millisecond},
			middleware: []middleware.Middleware{delay},
			customize: func(h *handler) {
				h.say = func(ctx context.Context, request *echov1.SayRequest) (*echov1.SayReply, error) {
					calls.Add(1)
					return h.base.Say(ctx, request)
				}
			},
		})
		_, api := h.dial(t)
		login(t, api)
		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()
		_, err := api.Say(ctx, &echov1.SayRequest{})
		reason(t, err, "TIMEOUT")
		receive(t, passed)
		if calls.Load() != 0 {
			t.Fatal("canceled Handler started")
		}
	})
}

func TestNotifyHasNoResponse(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()
		handled := make(chan struct{}, 1)
		h := start(t, mode, settings{customize: func(h *handler) {
			h.report = func(context.Context, *echov1.ReportRequest) error {
				handled <- struct{}{}
				return nil
			}
		}})
		wire := rawDial(t, h)
		rawSend(t, wire, mode.codec, protocol.Message{
			Type:      protocol.Request,
			Route:     echov1.EchoLoginRoute,
			RequestID: 1,
		}, &echov1.LoginRequest{Token: demoToken})
		if _, err := wire.Read(protocol.DefaultMaxSize, time.Now().Add(testTimeout)); err != nil {
			t.Fatal(err)
		}
		rawSend(t, wire, mode.codec, protocol.Message{Type: protocol.Notify, Route: echov1.EchoReportRoute}, &echov1.ReportRequest{})
		receive(t, handled)
		data, err := protocol.Encode(protocol.Message{Type: protocol.Heartbeat}, protocol.DefaultMaxSize)
		if err != nil {
			t.Fatal(err)
		}
		if err := wire.Write(data, time.Now().Add(testTimeout)); err != nil {
			t.Fatal(err)
		}
		message, err := wire.Read(protocol.DefaultMaxSize, time.Now().Add(testTimeout))
		if err != nil || message.Type != protocol.Heartbeat {
			t.Fatalf("notification produced a response: %v %v", message, err)
		}
	})
}
