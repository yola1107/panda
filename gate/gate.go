package gate

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
	"github.com/yola1107/panda/internal/transportio"

	"github.com/gorilla/websocket"
)

// Gate 实现 Kratos transport.Server；每个实例只能启动一次。
type Gate struct {
	internalv1.UnimplementedGateServer
	config      Config
	selector    Selector
	mu          sync.Mutex
	connections map[*connection]struct{}
	listeners   []net.Listener
	servers     []*http.Server
	addresses   []string
	ready       chan struct{}
	done        chan struct{}
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	started     bool
	stopped     bool
}

// New 校验配置；Start 时才绑定监听端口。
func New(config Config, options ...Option) (*Gate, error) {
	if config.ID == "" || len(config.Entries) == 0 {
		return nil, errors.New("Gate requires ID and entries")
	}

	config.Entries = append([]Entry(nil), config.Entries...)
	for i := range config.Entries {
		if err := config.Entries[i].defaults(); err != nil {
			return nil, err
		}
	}

	g := &Gate{
		config:      config,
		connections: make(map[*connection]struct{}),
		ready:       make(chan struct{}),
		done:        make(chan struct{}),
	}
	for _, option := range options {
		option(g)
	}

	return g, nil
}

// Ready 在全部入口已监听后关闭。
func (g *Gate) Ready() <-chan struct{} { return g.ready }

// Addresses 按配置顺序返回实际监听地址，WebSocket 地址包含路径。
func (g *Gate) Addresses() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return append([]string(nil), g.addresses...)
}

// Start 监听入口并等待停止，适合交给 kratos.App 运行。
func (g *Gate) Start(ctx context.Context) error {
	g.mu.Lock()
	if g.started || g.stopped {
		g.mu.Unlock()
		return errors.New("Gate already started or stopped")
	}

	g.started = true
	ctx, g.cancel = context.WithCancel(ctx)
	err := g.listen(ctx)
	g.mu.Unlock()

	if err != nil {
		g.shutdown()
		g.wg.Wait()
		close(g.done)

		return err
	}

	close(g.ready)
	<-ctx.Done()
	g.shutdown()
	g.wg.Wait()
	close(g.done)

	return nil
}

// Stop 取消连接及在途工作，等待传输 goroutine 退出。
func (g *Gate) Stop(ctx context.Context) error {
	g.mu.Lock()
	g.stopped = true
	cancel := g.cancel
	g.mu.Unlock()

	if cancel == nil {
		return nil
	}

	cancel()

	select {
	case <-g.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *Gate) listen(ctx context.Context) error {
	muxes := make(map[string]*http.ServeMux)
	wsAddresses := make(map[string]string)
	paths := make(map[string]bool)

	for _, entry := range g.config.Entries {
		if entry.Network == "websocket" {
			key := entry.Address + entry.Path
			if paths[key] {
				return errors.New("duplicate WebSocket entry")
			}

			paths[key] = true

			mux := muxes[entry.Address]
			if mux == nil {
				listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", entry.Address)
				if err != nil {
					return err
				}

				g.listeners = append(g.listeners, listener)
				mux = http.NewServeMux()
				muxes[entry.Address] = mux
				wsAddresses[entry.Address] = listener.Addr().String()
				server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
				g.servers = append(g.servers, server)
				g.wg.Go(func() { _ = server.Serve(listener) })
			}

			mux.HandleFunc(entry.Path, g.upgrade(ctx, entry))
			g.addresses = append(g.addresses, "ws://"+wsAddresses[entry.Address]+entry.Path)

			continue
		}

		listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", entry.Address)
		if err != nil {
			return err
		}

		g.listeners = append(g.listeners, listener)
		g.addresses = append(g.addresses, listener.Addr().String())
		g.wg.Go(func() { g.accept(ctx, listener, entry) })
	}

	return nil
}

func (g *Gate) upgrade(ctx context.Context, entry Entry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		g.attach(ctx, transportio.WebSocket(conn), entry)
	}
}

func (g *Gate) accept(ctx context.Context, listener net.Listener, entry Entry) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}

		g.attach(ctx, transportio.TCP(conn), entry)
	}
}

func (g *Gate) attach(ctx context.Context, wire *transportio.Connection, entry Entry) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.stopped || ctx.Err() != nil {
		_ = wire.Close()
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	c := newConnection(g, wire, entry, cancel)
	g.connections[c] = struct{}{}
	g.wg.Go(func() {
		c.run(ctx)
		g.mu.Lock()
		delete(g.connections, c)
		g.mu.Unlock()
	})
}

func (g *Gate) shutdown() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.stopped = true
	if g.cancel != nil {
		g.cancel()
	}

	for _, listener := range g.listeners {
		_ = listener.Close()
	}

	for _, server := range g.servers {
		_ = server.Close()
	}

	for c := range g.connections {
		c.close()
	}
}
