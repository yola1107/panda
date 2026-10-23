package client

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/yola1107/panda/internal/fault"
	"github.com/yola1107/panda/protocol"
)

const (
	sendQueued uint32 = iota
	sendStarted
	sendCanceled
)

type outgoing struct {
	data     []byte
	deadline time.Time
	result   chan error
	state    atomic.Uint32
}

// cancel 与 writer 争用发送权，返回消息是否已经开始写入。
func (o *outgoing) cancel() bool {
	if o.state.CompareAndSwap(sendQueued, sendCanceled) {
		return false
	}
	return o.state.Load() == sendStarted
}

func (c *Client) write(ctx context.Context, message protocol.Message) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	data, err := protocol.Encode(message, c.config.MaxSize)
	if err != nil {
		return false, err
	}

	deadline := time.Now().Add(5 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}

	item := &outgoing{data: data, deadline: deadline, result: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-c.done:
		return false, fault.Closed()
	case c.writes <- item:
	}

	select {
	case <-ctx.Done():
		return item.cancel(), ctx.Err()
	case <-c.done:
		return item.cancel(), fault.Closed()
	case err := <-item.result:
		if canceled := ctx.Err(); canceled != nil {
			return item.cancel(), canceled
		}
		return item.state.Load() == sendStarted, err
	}
}

func (c *Client) send() {
	defer c.shutdown()

	for {
		select {
		case <-c.done:
			return
		case item := <-c.writes:
			if !item.state.CompareAndSwap(sendQueued, sendStarted) {
				item.result <- context.Canceled
				continue
			}

			err := c.wire.Write(item.data, item.deadline)
			item.result <- err
			if err != nil {
				return
			}
		}
	}
}
