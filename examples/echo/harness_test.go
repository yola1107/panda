package echo_test

import (
	"context"
	"testing"
	"time"

	echov1 "github.com/yola1107/panda/api/echo/v1"
	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
	"github.com/yola1107/panda/client"
	"github.com/yola1107/panda/examples/echo"
	"github.com/yola1107/panda/gate"
	"github.com/yola1107/panda/internal/staticdiscovery"
	"github.com/yola1107/panda/node"
	"github.com/yola1107/panda/remote"

	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
)

const (
	demoToken   = "demo-token"
	protoCodec  = "proto"
	jsonCodec   = "json"
	tcpNetwork  = "tcp"
	wsNetwork   = "websocket"
	testTimeout = 3 * time.Second
)

type deployment struct {
	network, codec string
	separated      bool
}

func matrix(t *testing.T, test func(*testing.T, deployment)) {
	t.Helper()

	for _, separated := range []bool{false, true} {
		mode := "combined"
		if separated {
			mode = "separated"
		}
		for _, network := range []string{tcpNetwork, wsNetwork} {
			for _, codec := range []string{protoCodec, jsonCodec} {
				t.Run(mode+"/"+network+"/"+codec, func(t *testing.T) {
					t.Parallel()
					test(t, deployment{network: network, codec: codec, separated: separated})
				})
			}
		}
	}
}

type handler struct {
	base   *echo.Handler
	login  func(context.Context, *echov1.LoginRequest) (*echov1.LoginReply, string, error)
	say    func(context.Context, *echov1.SayRequest) (*echov1.SayReply, error)
	report func(context.Context, *echov1.ReportRequest) error
}

func (h *handler) Login(ctx context.Context, req *echov1.LoginRequest) (*echov1.LoginReply, string, error) {
	if h.login != nil {
		return h.login(ctx, req)
	}

	return h.base.Login(ctx, req)
}

func (h *handler) Say(ctx context.Context, req *echov1.SayRequest) (*echov1.SayReply, error) {
	if h.say != nil {
		return h.say(ctx, req)
	}

	return h.base.Say(ctx, req)
}

func (h *handler) Report(ctx context.Context, req *echov1.ReportRequest) error {
	if h.report != nil {
		return h.report(ctx, req)
	}

	return h.base.Report(ctx, req)
}

type settings struct {
	rpcOptions []kgrpc.ClientOption
	middleware []middleware.Middleware
	dualCodec  bool
	entry      gate.Entry
	nodeLimit  int
	codecs     []string
	customize  func(*handler)
}

type harness struct {
	node    *node.Node
	gate    *gate.Gate
	mode    deployment
	nodeRPC *kgrpc.Server
}

func start(t *testing.T, mode deployment, opts settings) *harness {
	t.Helper()

	n, err := node.New(node.Config{MaxExecuting: opts.nodeLimit, Middleware: opts.middleware})
	if err != nil {
		t.Fatal(err)
	}

	h := &handler{base: &echo.Handler{Node: n}}
	if opts.customize != nil {
		opts.customize(h)
	}

	if err := echov1.RegisterEcho(n, h, opts.codecs...); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		if err := n.Stop(ctx); err != nil {
			t.Error(err)
		}
	})

	result := &harness{node: n, mode: mode}
	selector := gate.Local(n)
	if mode.separated {
		selector = result.remoteNodes(t, opts.rpcOptions)
	}

	e := opts.entry
	e.Network, e.Codec, e.Address, e.Path = mode.network, mode.codec, "127.0.0.1:0", "/echo"
	e.Service, e.LoginRoute = echov1.EchoService, echov1.EchoLoginRoute

	entries := []gate.Entry{e}
	if opts.dualCodec {
		other := e
		other.Codec = jsonCodec
		if e.Codec == jsonCodec {
			other.Codec = protoCodec
		}
		other.Path = "/other"
		entries = append(entries, other)
	}

	g, err := gate.New(gate.Config{ID: t.Name(), Entries: entries}, gate.WithSelector(selector))
	if err != nil {
		t.Fatal(err)
	}

	result.gate = g
	if mode.separated {
		result.remotePush(t)
	} else {
		n.SetPusher(g)
	}

	runGate(t, g)
	return result
}

func (h *harness) remoteNodes(t *testing.T, options []kgrpc.ClientOption) gate.Selector {
	t.Helper()

	rpc := kgrpc.NewServer(kgrpc.Address("127.0.0.1:0"), kgrpc.Timeout(0), kgrpc.Middleware(tracing.Server()))
	internalv1.RegisterNodeServer(rpc, h.node)
	address := runRPC(t, rpc)
	h.nodeRPC = rpc
	d := staticdiscovery.Discovery{
		echov1.EchoService: {{
			ID:        "node",
			Name:      echov1.EchoService,
			Endpoints: []string{address},
		}},
	}

	nodes, err := remote.NewNodes(t.Context(), d, []string{echov1.EchoService}, options...)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(nodes.Close)
	return nodes
}

func (h *harness) remotePush(t *testing.T) {
	t.Helper()

	rpc := kgrpc.NewServer(kgrpc.Address("127.0.0.1:0"), kgrpc.Timeout(0), kgrpc.Middleware(tracing.Server()))
	internalv1.RegisterGateServer(rpc, h.gate)
	address := runRPC(t, rpc)
	pusher, err := remote.NewPusher(staticdiscovery.Discovery{
		remote.GateService: {{
			ID:        t.Name(),
			Name:      remote.GateService,
			Endpoints: []string{address},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.node.SetPusher(pusher)
}

func runRPC(t *testing.T, rpc *kgrpc.Server) string {
	t.Helper()

	endpoint, err := rpc.Endpoint()
	if err != nil {
		t.Fatal(err)
	}

	runServer(t, rpc)
	return endpoint.String()
}

func runGate(t *testing.T, g *gate.Gate) {
	t.Helper()

	runServer(t, g)
	select {
	case <-g.Ready():
	case <-time.After(testTimeout):
		t.Fatal("Gate startup timeout")
	}
}

func runServer(t *testing.T, server transport.Server) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- server.Start(t.Context())
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		if err := server.Stop(ctx); err != nil {
			t.Error(err)
		}
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
}

func (h *harness) dial(t *testing.T) (*client.Client, *echov1.EchoPandaClient) {
	t.Helper()

	c, err := client.Dial(t.Context(), client.Config{
		Network: h.mode.network,
		Address: h.gate.Addresses()[0],
		Codec:   h.mode.codec,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(c.Close)
	return c, echov1.NewEchoPandaClient(c)
}

func login(t *testing.T, api *echov1.EchoPandaClient) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	if _, err := api.Login(ctx, &echov1.LoginRequest{Token: demoToken}); err != nil {
		t.Fatal(err)
	}
}

func reason(t *testing.T, err error, want string) {
	t.Helper()

	if kerrors.Reason(err) != want {
		t.Fatalf("want reason %s, got %v", want, err)
	}
}

func receive[T any](t *testing.T, values <-chan T) T {
	t.Helper()

	select {
	case value := <-values:
		return value
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for observable result")
	}

	var zero T
	return zero
}
