package echo_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	echov1 "github.com/yola1107/panda/api/echo/v1"
	"github.com/yola1107/panda/client"
	"github.com/yola1107/panda/gate"
	"github.com/yola1107/panda/session"

	kerrors "github.com/go-kratos/kratos/v3/errors"
)

func TestTrustedContext(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()
		h := start(t, mode, settings{customize: func(h *handler) {
			h.login = func(ctx context.Context, req *echov1.LoginRequest) (*echov1.LoginReply, string, error) {
				_, authenticated := session.FromContext(ctx)
				c, ok := session.ConnectionFromContext(ctx)
				if authenticated || !ok || c.GateID != t.Name() || c.ID == "" || c.Service != echov1.EchoService || c.Codec != mode.codec {
					return nil, "", kerrors.InternalServer("INVALID_CONTEXT", "untrusted login context")
				}
				return h.base.Login(ctx, req)
			}
		}})
		_, api := h.dial(t)
		login(t, api)
	})
}

func TestSessionPushIsolation(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()
		sessions := make(chan session.Session, 1)
		h := start(t, mode, settings{customize: func(h *handler) {
			h.report = func(ctx context.Context, _ *echov1.ReportRequest) error {
				s, _ := session.FromContext(ctx)
				sessions <- s
				return nil
			}
		}})
		c, first := h.dial(t)
		_, second := h.dial(t)
		login(t, first)
		login(t, second)
		updates := make(chan string, 2)
		first.OnUpdate(func(_ context.Context, message *echov1.Update) error {
			updates <- "first:" + message.Text
			return nil
		})
		second.OnUpdate(func(_ context.Context, message *echov1.Update) error {
			updates <- "second:" + message.Text
			return nil
		})
		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()
		if err := first.Report(ctx, &echov1.ReportRequest{}); err != nil {
			t.Fatal(err)
		}
		target := receive(t, sessions)
		if err := echov1.PushEchoUpdate(ctx, h.node, target, &echov1.Update{Text: "event"}); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, updates); got != "first:event" {
			t.Fatal(got)
		}
		c.Close()
		waitSessionClosed(ctx, t, h, target)
		if _, err := second.Say(ctx, &echov1.SayRequest{Text: "alive"}); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-updates:
			t.Fatalf("misdelivered push: %s", got)
		default:
		}
	})
}

func waitSessionClosed(ctx context.Context, t *testing.T, h *harness, target session.Session) {
	t.Helper()
	for {
		err := echov1.PushEchoUpdate(ctx, h.node, target, &echov1.Update{Text: "closed"})
		if err != nil {
			if why := kerrors.Reason(err); why != "SESSION_CLOSED" && why != "DELIVERY_FAILED" {
				t.Fatalf("unexpected closed-session error: %v", err)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("closed session still accepts push")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestDualCodecEntries(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()
		h := start(t, mode, settings{dualCodec: true})
		_, first := h.dial(t)
		second := dialOtherCodec(t, h)
		login(t, first)
		login(t, second)
		if mode.network == wsNetwork {
			assertSharedPort(t, h.gate.Addresses())
		}
		for i, api := range []*echov1.EchoPandaClient{first, second} {
			checkCodecInteractions(t, api, []string{"first", "second"}[i])
		}
	})
}

func dialOtherCodec(t *testing.T, h *harness) *echov1.EchoPandaClient {
	t.Helper()
	codec := jsonCodec
	if h.mode.codec == jsonCodec {
		codec = protoCodec
	}
	c, err := client.Dial(t.Context(), client.Config{Network: h.mode.network, Address: h.gate.Addresses()[1], Codec: codec})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return echov1.NewEchoPandaClient(c)
}

func assertSharedPort(t *testing.T, addresses []string) {
	t.Helper()
	first, err := url.Parse(addresses[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := url.Parse(addresses[1])
	if err != nil {
		t.Fatal(err)
	}
	if first.Host != second.Host || first.Path == second.Path {
		t.Fatalf("WebSocket entries do not share port: %v", addresses)
	}
}

func checkCodecInteractions(t *testing.T, api *echov1.EchoPandaClient, text string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	reply, err := api.Say(ctx, &echov1.SayRequest{Text: text})
	if err != nil || reply.GetText() != text {
		t.Fatalf("codec request: %v %v", reply, err)
	}
	_, err = api.Say(ctx, &echov1.SayRequest{Text: "error"})
	reason(t, err, "DEMO_ERROR")
	updates := make(chan string, 1)
	api.OnUpdate(func(_ context.Context, update *echov1.Update) error {
		updates <- update.Text
		return nil
	})
	if err := api.Report(ctx, &echov1.ReportRequest{Text: text}); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, updates); got != text {
		t.Fatal(got)
	}
}

func TestKeepAlive(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()
		h := start(t, mode, settings{entry: gate.Entry{IdleTimeout: 200 * time.Millisecond}})
		c, api := h.dial(t)
		login(t, api)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		finished := make(chan error, 1)
		go func() {
			finished <- c.KeepAlive(ctx, 30*time.Millisecond)
		}()
		select {
		case <-c.Done():
			t.Fatal("heartbeats did not keep connection alive")
		case <-time.After(500 * time.Millisecond):
		}
		cancel()
		if err := receive(t, finished); err == nil {
			t.Fatal("KeepAlive ignored cancellation")
		}
	})
}

func TestCanceledHeartbeatBeforeSend(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()
		h := start(t, mode, settings{})
		c, api := h.dial(t)
		login(t, api)
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		for range 32 {
			if err := c.Heartbeat(canceled); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled heartbeat: %v", err)
			}
		}
		ctx, stop := context.WithTimeout(t.Context(), testTimeout)
		defer stop()
		if err := c.Heartbeat(ctx); err != nil {
			t.Fatalf("unsent heartbeat closed healthy connection: %v", err)
		}
	})
}
