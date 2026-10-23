// Package node 注册逻辑服务并执行经过身份校验的 Handler。
package node

import (
	"context"
	"errors"
	"fmt"
	"sync"

	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
	"github.com/yola1107/panda/codec"
	"github.com/yola1107/panda/internal/fault"
	"github.com/yola1107/panda/protocol"

	"github.com/go-kratos/kratos/v3/middleware"
	"google.golang.org/protobuf/proto"
)

// Config 指定实例执行上限和两种部署共用的 Handler 中间件。
type Config struct {
	MaxExecuting int
	Middleware   []middleware.Middleware
}

// Result 将登录身份与客户端业务应答分离；普通 Handler 不应填写 UserID。
type Result struct {
	Message proto.Message
	UserID  string
}

// Route 将数字路由绑定到消息构造器和 Handler。
type Route struct {
	Number uint32
	Type   protocol.Type
	Login  bool
	New    func() proto.Message
	Handle func(context.Context, proto.Message) (Result, error)
}

type service struct {
	codecs map[string]bool
	routes map[uint32]Route
}

// Node 可交给 kratos.App 管理；注册必须在处理首个请求之前完成。
type Node struct {
	stopped chan struct{}
	pusher  Pusher
	internalv1.UnimplementedNodeServer
	mu         sync.RWMutex
	services   map[string]service
	started    bool
	slots      chan struct{}
	done       chan struct{}
	once       sync.Once
	workers    sync.WaitGroup
	middleware middleware.Middleware
}

// New 创建有界执行器；零上限使用 256，负值无效。
func New(config Config) (*Node, error) {
	if config.MaxExecuting == 0 {
		config.MaxExecuting = 256
	}

	if config.MaxExecuting < 1 {
		return nil, errors.New("Node execution limit must be positive")
	}

	return &Node{
		services:   make(map[string]service),
		slots:      make(chan struct{}, config.MaxExecuting),
		done:       make(chan struct{}),
		stopped:    make(chan struct{}),
		middleware: middleware.Chain(config.Middleware...),
	}, nil
}

// Register 原子注册逻辑服务，拒绝重复编号和多个登录路由。
func (n *Node) Register(name string, codecs []string, routes ...Route) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.started {
		return errors.New("Node registration is closed")
	}

	if _, exists := n.services[name]; exists || name == "" {
		return errors.New("invalid or duplicate logical service")
	}

	s := service{codecs: make(map[string]bool), routes: make(map[uint32]Route)}
	for _, name := range codecs {
		if _, err := codec.Lookup(name); err != nil {
			return err
		}

		s.codecs[name] = true
	}

	if len(s.codecs) == 0 {
		return errors.New("logical service needs a codec")
	}

	logins := 0

	for _, route := range routes {
		if err := addRoute(&s, route); err != nil {
			return err
		}

		if route.Login {
			logins++
		}
	}

	if logins != 1 {
		return errors.New("logical service needs exactly one login route")
	}

	n.services[name] = s

	return nil
}

func addRoute(s *service, route Route) error {
	if route.Number == 0 {
		return errors.New("route zero is reserved")
	}

	if _, ok := s.routes[route.Number]; ok {
		return fmt.Errorf("duplicate route %d", route.Number)
	}

	if route.Type != protocol.Request && route.Type != protocol.Notify && route.Type != protocol.Push {
		return errors.New("invalid route type")
	}

	if route.Login && route.Type != protocol.Request {
		return errors.New("login must be a request")
	}

	if route.Type != protocol.Push && (route.New == nil || route.Handle == nil) {
		return errors.New("route requires a message factory and Handler")
	}

	s.routes[route.Number] = route

	return nil
}

// Start 关闭注册并等待应用或 Node 停止。
func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	n.started = true
	n.mu.Unlock()

	select {
	case <-ctx.Done():
	case <-n.done:
	}

	return n.Stop(ctx)
}

// Stop 取消执行并等待实际 Handler 退出，等待受 ctx 约束。
func (n *Node) Stop(ctx context.Context) error {
	n.mu.Lock()
	n.once.Do(func() {
		close(n.done)
		go func() {
			n.workers.Wait()
			close(n.stopped)
		}()
	})
	n.mu.Unlock()

	select {
	case <-n.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Done 在 Node 停止时关闭，Gate 据此断开绑定会话。
func (n *Node) Done() <-chan struct{} { return n.done }

// Forward 对本地与 RPC 转交执行相同的验证、中间件和期限处理。
func (n *Node) Forward(ctx context.Context, req *internalv1.ForwardRequest) (*internalv1.ForwardReply, error) {
	if err := ctx.Err(); err != nil {
		return nil, fault.Normalize(err)
	}

	route, err := n.resolve(req)
	if err != nil {
		return nil, err
	}

	n.mu.Lock()
	select {
	case <-n.done:
		n.mu.Unlock()
		return nil, fault.Closed()
	default:
	}

	select {
	case n.slots <- struct{}{}:
	default:
		n.mu.Unlock()
		return nil, fault.Overloaded()
	}

	n.workers.Add(1)
	n.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type outcome struct {
		reply *internalv1.ForwardReply
		err   error
	}

	finished := make(chan outcome, 1)

	go func() {
		defer n.workers.Done()
		defer func() {
			<-n.slots
		}()

		reply, err := n.execute(ctx, req, route)
		finished <- outcome{reply, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fault.Normalize(ctx.Err())
	case <-n.done:
		return nil, fault.Closed()
	case result := <-finished:
		if err := ctx.Err(); err != nil {
			return nil, fault.Normalize(err)
		}

		return result.reply, result.err
	}
}
