// Package remote 将内部 Gate/Node 契约接入 Kratos gRPC 与服务发现。
package remote

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"slices"
	"sync"
	"time"

	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
	"github.com/yola1107/panda/gate"
	"github.com/yola1107/panda/internal/fault"

	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/registry"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// Nodes 按服务发现快照选择 Node，并监控实例移除和 RPC 断连。
type Nodes struct {
	watchers []registry.Watcher
	options  []kgrpc.ClientOption

	mu       sync.Mutex
	services map[string]map[string]string
	ready    map[string]chan struct{}
	backends map[string]*backend
	stopped  bool
	next     uint64

	cancel  context.CancelFunc
	done    chan struct{}
	workers sync.WaitGroup
}

// NewNodes 监听给定逻辑服务。ctx 属于组件生命周期，Close 负责取消并等待。
func NewNodes(ctx context.Context, discovery registry.Discovery, services []string, options ...kgrpc.ClientOption) (*Nodes, error) {
	if discovery == nil {
		return nil, errors.New("node discovery is required")
	}

	ctx, cancel := context.WithCancel(ctx)
	n := &Nodes{
		services: make(map[string]map[string]string),
		ready:    make(map[string]chan struct{}),
		backends: make(map[string]*backend),
		options:  append([]kgrpc.ClientOption(nil), options...),
		cancel:   cancel,
		done:     make(chan struct{}),
	}

	var jobs []func()
	for _, service := range services {
		if _, ok := n.ready[service]; ok {
			continue
		}

		watcher, err := discovery.Watch(ctx, service)
		if err != nil {
			n.Close()
			return nil, err
		}

		n.watchers = append(n.watchers, watcher)
		n.ready[service] = make(chan struct{})
		n.services[service] = make(map[string]string)
		jobs = append(jobs, func() { n.watch(service, watcher) })
	}

	for _, job := range jobs {
		n.workers.Go(job)
	}
	n.workers.Go(func() { n.monitor(ctx) })

	return n, nil
}

// Select 选择后固定调用实例地址，后续请求不再经过负载均衡。
func (n *Nodes) Select(ctx context.Context, service string) (gate.Backend, error) {
	n.mu.Lock()
	ready := n.ready[service]
	n.mu.Unlock()

	if ready == nil {
		return nil, kerrors.NotFound("UNKNOWN_SERVICE", "service not watched")
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-n.done:
		return nil, fault.Closed()
	case <-ready:
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.stopped {
		return nil, fault.Closed()
	}

	instances := n.services[service]
	ids := slices.Sorted(maps.Keys(instances))
	if len(ids) == 0 {
		return nil, kerrors.ServiceUnavailable("NO_NODE", "no Node instance available")
	}

	id := ids[n.next%uint64(len(ids))]
	n.next++

	key := service + "/" + id
	if b := n.backends[key]; b != nil {
		return b, nil
	}

	conn, err := dial(ctx, instances[id], n.options)
	if err != nil {
		return nil, err
	}

	b := &backend{conn: conn, client: internalv1.NewNodeClient(conn), done: make(chan struct{})}
	n.backends[key] = b

	return b, nil
}

// Close 释放发现监听、RPC 连接和监控 goroutine。
func (n *Nodes) Close() {
	n.mu.Lock()
	if !n.stopped {
		n.stopped = true
		close(n.done)
	}

	n.cancel()
	for _, watcher := range n.watchers {
		_ = watcher.Stop()
	}
	for _, b := range n.backends {
		b.close()
	}
	n.mu.Unlock()

	n.workers.Wait()
}

func (n *Nodes) watch(service string, watcher registry.Watcher) {
	first := true

	for {
		instances, err := watcher.Next()

		n.mu.Lock()
		n.update(service, instances)

		if first {
			close(n.ready[service])
			first = false
		}
		n.mu.Unlock()

		if err != nil {
			return
		}
	}
}

func (n *Nodes) update(service string, instances []*registry.ServiceInstance) {
	addresses := make(map[string]string)
	for _, instance := range instances {
		if instance == nil || instance.ID == "" {
			continue
		}

		if address := endpoint(instance); address != "" {
			addresses[instance.ID] = address
		}
	}

	for id, oldAddress := range n.services[service] {
		if addresses[id] == oldAddress {
			continue
		}

		key := service + "/" + id
		if b := n.backends[key]; b != nil {
			b.close()
			delete(n.backends, key)
		}
	}

	n.services[service] = addresses
}

func (n *Nodes) monitor(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.checkConnections(ctx)
		}
	}
}

func endpoint(instance *registry.ServiceInstance) string {
	for _, address := range instance.Endpoints {
		u, err := url.Parse(address)
		if err == nil && u.Scheme == "grpc" && u.Host != "" {
			return u.Host
		}
	}

	return ""
}

func dial(ctx context.Context, address string, options []kgrpc.ClientOption) (*grpc.ClientConn, error) {
	base := []kgrpc.ClientOption{
		kgrpc.WithEndpoint(address),
		kgrpc.WithTimeout(0),
		kgrpc.WithMiddleware(tracing.Client()),
		kgrpc.WithOptions(grpc.WithDisableRetry()),
	}
	return kgrpc.NewClient(ctx, slices.Concat(base, options)...)
}

func (n *Nodes) checkConnections(ctx context.Context) {
	n.mu.Lock()
	backends := maps.Clone(n.backends)
	n.mu.Unlock()

	for key, b := range backends {
		if b.conn.GetState() == connectivity.Ready {
			b.ready.Store(true)
			continue
		}
		if !b.ready.Load() || healthy(ctx, b.conn) {
			continue
		}

		n.mu.Lock()
		if n.backends[key] == b {
			b.close()
			delete(n.backends, key)
		}
		n.mu.Unlock()
	}
}

func healthy(ctx context.Context, conn *grpc.ClientConn) bool {
	// Idle 可能来自正常空闲；先唤醒连接，再用独立健康 RPC 确认。
	conn.Connect()
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	reply, err := grpc_health_v1.NewHealthClient(conn).Check(
		ctx,
		&grpc_health_v1.HealthCheckRequest{},
		grpc.WaitForReady(true),
	)
	return err == nil && reply.Status == grpc_health_v1.HealthCheckResponse_SERVING
}
