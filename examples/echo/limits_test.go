package echo_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	echov1 "github.com/yola1107/panda/api/echo/v1"
	"github.com/yola1107/panda/gate"
)

const (
	slowText = "slow"
	nextText = "next"
)

func TestExecutionTimeout(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		canceled := make(chan struct{}, 1)
		h := start(t, mode, settings{
			entry: gate.Entry{ExecutionTimeout: 100 * time.Millisecond},
			customize: func(h *handler) {
				h.say = func(ctx context.Context, req *echov1.SayRequest) (*echov1.SayReply, error) {
					if req.Text != slowText {
						return h.base.Say(ctx, req)
					}

					<-ctx.Done()
					canceled <- struct{}{}
					return &echov1.SayReply{Text: "late"}, nil
				}
			},
		})
		_, api := h.dial(t)
		login(t, api)

		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()

		_, err := api.Say(ctx, &echov1.SayRequest{Text: slowText})
		reason(t, err, "TIMEOUT")
		receive(t, canceled)

		reply, err := api.Say(ctx, &echov1.SayRequest{Text: nextText})
		if err != nil || reply.GetText() != nextText {
			t.Fatalf("late result affected next request: %v %v", reply, err)
		}
	})
}

func TestLateLogin(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		release := make(chan struct{})
		exited := make(chan struct{})
		h := start(t, mode, settings{
			entry: gate.Entry{ExecutionTimeout: 100 * time.Millisecond},
			customize: func(h *handler) {
				h.login = func(ctx context.Context, req *echov1.LoginRequest) (*echov1.LoginReply, string, error) {
					if req.Token != slowText {
						return h.base.Login(ctx, req)
					}

					<-release
					close(exited)
					return &echov1.LoginReply{}, "late-user", nil
				}
			},
		})
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		_, api := h.dial(t)

		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()

		_, err := api.Login(ctx, &echov1.LoginRequest{Token: slowText})
		reason(t, err, "TIMEOUT")
		close(release)
		receive(t, exited)

		_, err = api.Say(ctx, &echov1.SayRequest{})
		reason(t, err, "UNAUTHENTICATED")
		login(t, api)
	})
}

func TestConnectionLimitAndCancellation(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		entered, exited := make(chan struct{}, 1), make(chan struct{}, 1)
		h := start(t, mode, settings{entry: gate.Entry{MaxInFlight: 1}, customize: func(h *handler) {
			h.say = func(ctx context.Context, _ *echov1.SayRequest) (*echov1.SayReply, error) {
				entered <- struct{}{}
				<-ctx.Done()
				exited <- struct{}{}
				return nil, ctx.Err()
			}
		}})
		c, api := h.dial(t)
		login(t, api)

		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()

		first := make(chan error, 1)
		go func() {
			_, err := api.Say(ctx, &echov1.SayRequest{})
			first <- err
		}()

		receive(t, entered)

		_, err := api.Say(ctx, &echov1.SayRequest{})
		reason(t, err, "OVERLOADED")

		if err := api.Report(ctx, &echov1.ReportRequest{}); err != nil {
			t.Fatal(err)
		}

		receive(t, c.Done())
		receive(t, exited)

		if receive(t, first) == nil {
			t.Fatal("closed request succeeded")
		}
	})
}

func TestNodeLimitRetainedUntilHandlerExit(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		release, exited := make(chan struct{}), make(chan struct{})

		var running, maximum atomic.Int32

		h := start(t, mode, settings{
			nodeLimit: 1,
			entry:     gate.Entry{ExecutionTimeout: 100 * time.Millisecond},
			customize: func(h *handler) {
				h.say = func(ctx context.Context, req *echov1.SayRequest) (*echov1.SayReply, error) {
					count := running.Add(1)
					defer running.Add(-1)
					maximum.Store(max(maximum.Load(), count))

					if req.Text == slowText {
						<-release
						close(exited)
					}

					return h.base.Say(ctx, req)
				}
			},
		})
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		_, first := h.dial(t)
		other, second := h.dial(t)
		login(t, first)
		login(t, second)

		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()

		_, err := first.Say(ctx, &echov1.SayRequest{Text: slowText})
		reason(t, err, "TIMEOUT")
		_, err = second.Say(ctx, &echov1.SayRequest{})
		reason(t, err, "OVERLOADED")

		if err := second.Report(ctx, &echov1.ReportRequest{}); err != nil {
			t.Fatal(err)
		}

		receive(t, other.Done())
		close(release)
		receive(t, exited)

		if maximum.Load() != 1 {
			t.Fatalf("Node executed %d handlers concurrently", maximum.Load())
		}
	})
}

func TestClientCancellation(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		entered, release := make(chan struct{}, 1), make(chan struct{})

		var calls atomic.Int32

		h := start(t, mode, settings{customize: func(h *handler) {
			h.say = func(ctx context.Context, req *echov1.SayRequest) (*echov1.SayReply, error) {
				calls.Add(1)

				if req.Text == slowText {
					entered <- struct{}{}

					<-release
				}

				return h.base.Say(ctx, req)
			}
		}})
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		_, api := h.dial(t)
		login(t, api)
		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)

		go func() {
			_, err := api.Say(ctx, &echov1.SayRequest{Text: slowText})
			result <- err
		}()

		receive(t, entered)
		cancel()

		if err := receive(t, result); !errors.Is(err, context.Canceled) {
			t.Fatalf("call cancellation: %v", err)
		}

		close(release)

		ctx, stop := context.WithTimeout(t.Context(), testTimeout)
		defer stop()

		reply, err := api.Say(ctx, &echov1.SayRequest{Text: nextText})
		if err != nil || reply.GetText() != nextText {
			t.Fatalf("late response misrouted: %v %v", reply, err)
		}

		if calls.Load() != 2 {
			t.Fatalf("requests retried: %d", calls.Load())
		}
	})
}
