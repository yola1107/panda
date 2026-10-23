package gate

import (
	"context"
	"sync"
	"time"

	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
	"github.com/yola1107/panda/internal/transportio"
	"github.com/yola1107/panda/protocol"

	"github.com/google/uuid"
)

type connection struct {
	gate         *Gate
	id           string
	wire         *transportio.Connection
	entry        Entry
	authDeadline time.Time

	mu      sync.Mutex
	closed  bool
	backend Backend
	session *internalv1.Session
	login   bool

	cancel  context.CancelFunc
	done    chan struct{}
	send    chan []byte
	slots   chan struct{}
	workers sync.WaitGroup
}

func newConnection(gate *Gate, wire *transportio.Connection, entry Entry, cancel context.CancelFunc) *connection {
	return &connection{
		done:         make(chan struct{}),
		gate:         gate,
		id:           uuid.NewString(),
		slots:        make(chan struct{}, entry.MaxInFlight),
		authDeadline: time.Now().Add(entry.AuthTimeout),
		wire:         wire,
		entry:        entry,
		cancel:       cancel,
		send:         make(chan []byte, entry.SendQueue),
	}
}

func (c *connection) close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}

	c.closed = true
	close(c.done)
	c.cancel()
	_ = c.wire.Close()
}

func (c *connection) run(ctx context.Context) {
	c.workers.Go(func() { c.write(ctx) })
	defer c.workers.Wait()

	timer := time.AfterFunc(c.entry.AuthTimeout, c.expireAuthentication)
	defer timer.Stop()
	defer c.close()

	for {
		m, err := c.wire.Read(c.entry.MaxSize, time.Now().Add(c.entry.IdleTimeout))
		if err != nil {
			return
		}

		if m.Type == protocol.Heartbeat {
			if !c.enqueue(m) {
				return
			}

			continue
		}

		if m.Type != protocol.Request && m.Type != protocol.Notify {
			return
		}

		c.dispatch(ctx, m)
	}
}

func (c *connection) enqueue(m protocol.Message) bool {
	data, err := protocol.Encode(m, c.entry.MaxSize)
	if err != nil {
		c.close()
		return false
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return false
	}

	select {
	case c.send <- data:
		c.mu.Unlock()
		return true
	default:
		c.mu.Unlock()
		c.close()
		return false
	}
}

func (c *connection) write(ctx context.Context) {
	defer c.close()

	for {
		select {
		case <-ctx.Done():
			return
		case data := <-c.send:
			if err := c.wire.Write(data, time.Now().Add(c.entry.ExecutionTimeout)); err != nil {
				return
			}
		}
	}
}
