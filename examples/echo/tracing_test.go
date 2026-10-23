package echo_test

import (
	"context"
	"os"
	"testing"

	echov1 "github.com/yola1107/panda/api/echo/v1"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var spanRecorder = tracetest.NewSpanRecorder()

func TestMain(m *testing.M) {
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(provider)
	code := m.Run()
	if err := provider.Shutdown(context.Background()); err != nil {
		code = 1
	}
	os.Exit(code)
}

func TestTracing(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()
		h := start(t, mode, settings{customize: func(h *handler) {
			h.say = func(ctx context.Context, _ *echov1.SayRequest) (*echov1.SayReply, error) {
				return &echov1.SayReply{Text: oteltrace.SpanContextFromContext(ctx).TraceID().String()}, nil
			}
		}})
		_, api := h.dial(t)
		login(t, api)
		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()
		reply, err := api.Say(ctx, &echov1.SayRequest{})
		if err != nil {
			t.Fatal(err)
		}
		assertTrace(t, reply.Text, mode.separated)
	})
}

func assertTrace(t *testing.T, id string, separated bool) {
	t.Helper()
	foundGate, foundNode, foundRPC := false, false, false
	for _, span := range spanRecorder.Ended() {
		if span.SpanContext().TraceID().String() != id {
			continue
		}
		switch span.Name() {
		case "/gate/echo.v1.Echo/2":
			foundGate = true
			if span.Parent().IsValid() {
				t.Fatal("client trace metadata crossed Gate boundary")
			}
		case "/node/echo.v1.Echo/2":
			foundNode = span.Parent().IsValid()
		case "/panda.internal.v1.Node/Forward":
			foundRPC = span.Parent().IsValid()
		}
	}
	if !foundGate || !foundNode || (separated && !foundRPC) {
		t.Fatalf("trace chain incomplete: Gate=%t Node=%t RPC=%t", foundGate, foundNode, foundRPC)
	}
}
