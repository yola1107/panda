// Package client 提供 TCP 和 WebSocket 的 Go 参考客户端。
package client

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/yola1107/panda/codec"
	"github.com/yola1107/panda/internal/fault"
	"github.com/yola1107/panda/internal/transportio"
	"github.com/yola1107/panda/protocol"

	"github.com/go-kratos/kratos/v3/encoding"
	kerrors "github.com/go-kratos/kratos/v3/errors"
	"google.golang.org/protobuf/proto"
)

// Config 指定入口地址和双方约定的 Codec。
type Config struct {
	Network string
	Address string
	Codec   string
	MaxSize int
}

// Client 串行写入，允许多个请求并行等待。
type Client struct {
	writes        chan *outgoing
	writerDone    chan struct{}
	callbacks     map[uint32]func(context.Context, []byte) error
	pushes        chan protocol.Message
	pushDone      chan struct{}
	cancel        context.CancelFunc
	config        Config
	wire          *transportio.Connection
	heartbeatGate chan struct{}
	heartbeats    chan struct{}
	done          chan struct{}
	readerDone    chan struct{}
	once          sync.Once
	mu            sync.Mutex
	next          uint32
	pending       map[uint32]chan protocol.Message
}

// Dial 建立连接；ctx 只约束建连，连接生命周期由 Close 管理。
func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.MaxSize == 0 {
		config.MaxSize = protocol.DefaultMaxSize
	}

	if config.MaxSize < protocol.HeaderSize {
		return nil, errors.New("maximum size is smaller than envelope header")
	}

	if _, err := codec.Lookup(config.Codec); err != nil {
		return nil, err
	}

	wire, err := transportio.Dial(ctx, config.Network, config.Address)
	if err != nil {
		return nil, err
	}

	lifetime, cancel := context.WithCancel(context.Background())

	c := &Client{
		config:        config,
		wire:          wire,
		cancel:        cancel,
		writes:        make(chan *outgoing, 64),
		writerDone:    make(chan struct{}),
		callbacks:     make(map[uint32]func(context.Context, []byte) error),
		pushes:        make(chan protocol.Message, 64),
		pushDone:      make(chan struct{}),
		pending:       make(map[uint32]chan protocol.Message),
		heartbeats:    make(chan struct{}, 1),
		heartbeatGate: make(chan struct{}, 1),
		done:          make(chan struct{}),
		readerDone:    make(chan struct{}),
	}
	go c.read()
	go c.send()
	go c.dispatch(lifetime)

	return c, nil
}

// Close 关闭连接，等待读写及响应取消的推送回调退出。
func (c *Client) Close() {
	c.shutdown()
	<-c.readerDone
	<-c.writerDone
	<-c.pushDone
}

// Done 在连接关闭时关闭。
func (c *Client) Done() <-chan struct{} { return c.done }

// Heartbeat 发送应用层心跳并等待 Gate 回复。
func (c *Client) Heartbeat(ctx context.Context) error {
	select {
	case c.heartbeatGate <- struct{}{}:
		defer func() {
			<-c.heartbeatGate
		}()
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return fault.Closed()
	}

	if started, err := c.write(ctx, protocol.Message{Type: protocol.Heartbeat}); err != nil {
		if started {
			c.shutdown()
		}
		return err
	}

	select {
	case <-ctx.Done():
		c.shutdown()
		return ctx.Err()
	case <-c.done:
		return context.Canceled
	case <-c.heartbeats:
		if err := ctx.Err(); err != nil {
			c.shutdown()
			return err
		}
		return nil
	}
}

func (c *Client) shutdown() {
	c.once.Do(func() {
		close(c.done)
		c.cancel()
		_ = c.wire.Close()
	})
}

func (c *Client) read() {
	defer close(c.readerDone)
	defer c.shutdown()

	for {
		m, err := c.wire.Read(c.config.MaxSize, time.Time{})
		if err != nil {
			return
		}

		if !c.receive(m) {
			return
		}
	}
}

// Request 关联 RequestID，取消只结束本地等待，不重试业务调用。
func (c *Client) Request(ctx context.Context, route uint32, request, reply proto.Message) error {
	encoding, err := codec.Lookup(c.config.Codec)
	if err != nil {
		return err
	}

	payload, err := encoding.Marshal(request)
	if err != nil {
		return err
	}

	c.mu.Lock()
	if c.next == math.MaxUint32 {
		c.mu.Unlock()
		return errors.New("RequestID exhausted; reconnect required")
	}

	c.next++
	id := c.next
	ch := make(chan protocol.Message, 1)
	c.pending[id] = ch

	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if _, err := c.write(ctx, protocol.Message{
		Type:      protocol.Request,
		Route:     route,
		RequestID: id,
		Payload:   payload,
	}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return fault.Closed()
	case m := <-ch:
		if err := ctx.Err(); err != nil {
			return err
		}
		if m.Route != route {
			return errors.New("response route mismatch")
		}

		return decodeReply(encoding, m, reply)
	}
}

// Notify 发送单向通知，不等待业务应答。
func (c *Client) Notify(ctx context.Context, route uint32, message proto.Message) error {
	encoding, err := codec.Lookup(c.config.Codec)
	if err != nil {
		return err
	}

	payload, err := encoding.Marshal(message)
	if err != nil {
		return err
	}

	_, err = c.write(ctx, protocol.Message{Type: protocol.Notify, Route: route, Payload: payload})
	return err
}

func (c *Client) receive(m protocol.Message) bool {
	switch m.Type {
	case protocol.Response:
		c.mu.Lock()
		ch := c.pending[m.RequestID]
		delete(c.pending, m.RequestID)
		c.mu.Unlock()

		if ch != nil {
			ch <- m
		}
	case protocol.Push:
		select {
		case c.pushes <- m:
		default:
			return false
		}
	case protocol.Heartbeat:
		select {
		case c.heartbeats <- struct{}{}:
		default:
		}
	case protocol.Request, protocol.Notify:
		return false
	}

	return true
}

func decodeReply(encoding encoding.Codec, m protocol.Message, reply proto.Message) error {
	if m.Code != 0 {
		failure := &kerrors.Error{}
		if err := encoding.Unmarshal(m.Payload, &failure.Status); err != nil {
			return err
		}

		if failure.Code != m.Code {
			return errors.New("response error code mismatch")
		}

		return failure
	}

	return encoding.Unmarshal(m.Payload, reply)
}
