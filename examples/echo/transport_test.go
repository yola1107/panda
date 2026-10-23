package echo_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	echov1 "github.com/yola1107/panda/api/echo/v1"
	"github.com/yola1107/panda/codec"
	"github.com/yola1107/panda/gate"
	"github.com/yola1107/panda/internal/transportio"
	"github.com/yola1107/panda/protocol"
	"github.com/yola1107/panda/session"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
)

func TestAuthenticationTimeout(t *testing.T) {
	t.Parallel()

	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		h := start(t, mode, settings{entry: gate.Entry{AuthTimeout: 150 * time.Millisecond}})
		c, _ := h.dial(t)

		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()

		for {
			if err := c.Heartbeat(ctx); err != nil {
				break
			}

			select {
			case <-c.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}

		receive(t, c.Done())
	})
}

func TestIdleTimeout(t *testing.T) {
	t.Parallel()

	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		h := start(t, mode, settings{entry: gate.Entry{IdleTimeout: 200 * time.Millisecond}})
		c, api := h.dial(t)
		login(t, api)
		receive(t, c.Done())
	})
}

func TestUnsupportedCodec(t *testing.T) {
	t.Parallel()

	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		supported := jsonCodec
		if mode.codec == jsonCodec {
			supported = protoCodec
		}

		h := start(t, mode, settings{codecs: []string{supported}})
		_, api := h.dial(t)

		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()

		_, err := api.Login(ctx, &echov1.LoginRequest{Token: demoToken})
		reason(t, err, "UNSUPPORTED_CODEC")
	})
}

func TestOversizedFrame(t *testing.T) {
	t.Parallel()

	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		h := start(t, mode, settings{entry: gate.Entry{MaxSize: 128}})
		wire := rawDial(t, h)

		data, err := protocol.Encode(protocol.Message{
			Type:      protocol.Request,
			Route:     echov1.EchoLoginRoute,
			RequestID: 1,
			Payload:   make([]byte, 256),
		}, 512)
		if err != nil {
			t.Fatal(err)
		}

		if err := wire.Write(data, time.Now().Add(testTimeout)); err != nil {
			t.Fatal(err)
		}

		if _, err := wire.Read(512, time.Now().Add(testTimeout)); err == nil {
			t.Fatal("oversized frame was accepted")
		}
	})
}

func rawDial(t *testing.T, h *harness) *transportio.Connection {
	t.Helper()

	wire, err := transportio.Dial(t.Context(), h.mode.network, h.gate.Addresses()[0])
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = wire.Close() })
	return wire
}

func rawSend(t *testing.T, wire *transportio.Connection, codecName string, m protocol.Message, payload any) {
	t.Helper()

	encoding, err := codec.Lookup(codecName)
	if err != nil {
		t.Fatal(err)
	}

	m.Payload, err = encoding.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	data, err := protocol.Encode(m, protocol.DefaultMaxSize)
	if err != nil {
		t.Fatal(err)
	}

	if err := wire.Write(data, time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}
}

func TestSlowClient(t *testing.T) {
	t.Parallel()

	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		sessions := make(chan session.Session, 1)
		h := start(t, mode, settings{
			entry: gate.Entry{SendQueue: 1},
			customize: func(h *handler) {
				h.report = func(ctx context.Context, _ *echov1.ReportRequest) error {
					s, _ := session.FromContext(ctx)
					sessions <- s
					return nil
				}
			},
		})
		wire := rawDial(t, h)
		rawSend(t, wire, mode.codec, protocol.Message{
			Type:      protocol.Request,
			Route:     echov1.EchoLoginRoute,
			RequestID: 1,
		}, &echov1.LoginRequest{Token: demoToken})

		if m, err := wire.Read(protocol.DefaultMaxSize, time.Now().Add(testTimeout)); err != nil || m.Code != 0 {
			t.Fatalf("raw login: %v %v", m, err)
		}

		rawSend(t, wire, mode.codec, protocol.Message{Type: protocol.Notify, Route: echov1.EchoReportRoute}, &echov1.ReportRequest{})
		target := receive(t, sessions)
		other, api := h.dial(t)
		login(t, api)

		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()

		fillSendQueue(ctx, t, h, target)

		if err := other.Heartbeat(ctx); err != nil {
			t.Fatalf("slow client affected healthy client: %v", err)
		}

		drainClosed(t, wire)
	})
}

func TestNodeOffline(t *testing.T) {
	t.Parallel()

	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		h := start(t, mode, settings{})
		c, api := h.dial(t)
		login(t, api)

		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()

		if mode.separated {
			if err := h.nodeRPC.Stop(ctx); err != nil {
				t.Fatal(err)
			}
		} else if err := h.node.Stop(ctx); err != nil {
			t.Fatal(err)
		}

		receive(t, c.Done())
	})
}

func fillSendQueue(ctx context.Context, t *testing.T, h *harness, target session.Session) {
	t.Helper()

	message := &echov1.Update{Text: strings.Repeat("x", 60*1024)}
	for range 1024 {
		err := echov1.PushEchoUpdate(ctx, h.node, target, message)
		if err == nil {
			continue
		}

		if reason := kerrors.Reason(err); reason != "DELIVERY_FAILED" && reason != "SESSION_CLOSED" {
			t.Fatalf("unexpected push error: %v", err)
		}

		return
	}

	t.Fatal("slow client never filled send queue")
}

func drainClosed(t *testing.T, wire *transportio.Connection) {
	t.Helper()

	for {
		_, err := wire.Read(protocol.DefaultMaxSize, time.Now().Add(testTimeout))
		if err == nil {
			continue
		}

		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			t.Fatal("connection remained open")
		}

		return
	}
}

func TestIdleRPCPreservesSession(t *testing.T) {
	t.Parallel()

	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		if !mode.separated {
			t.Skip("RPC idle applies to separated deployment")
		}
		h := start(t, mode, settings{
			rpcOptions: []kgrpc.ClientOption{kgrpc.WithOptions(grpc.WithIdleTimeout(50 * time.Millisecond))},
		})
		c, api := h.dial(t)
		login(t, api)

		select {
		case <-c.Done():
			t.Fatal("healthy idle Node disconnected session")
		case <-time.After(300 * time.Millisecond):
		}

		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()
		if _, err := api.Say(ctx, &echov1.SayRequest{Text: "still bound"}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestWebSocketTextRejected(t *testing.T) {
	t.Parallel()

	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()

		if mode.network != wsNetwork {
			t.Skip("text frames only exist in WebSocket")
		}
		h := start(t, mode, settings{})
		conn := dialWebSocket(t, h)

		if err := conn.WriteMessage(websocket.TextMessage, []byte("heartbeat")); err != nil {
			t.Fatal(err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(testTimeout)); err != nil {
			t.Fatal(err)
		}
		_, _, err := conn.ReadMessage()
		if err == nil {
			t.Fatal("text frame accepted")
		}

		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			t.Fatal("text frame did not close connection")
		}
	})
}

func dialWebSocket(t *testing.T, h *harness) *websocket.Conn {
	t.Helper()

	conn, response, err := websocket.DefaultDialer.DialContext(t.Context(), h.gate.Addresses()[0], nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
