package client

import (
	"context"
	"errors"
	"time"

	"github.com/yola1107/panda/internal/fault"
	"github.com/yola1107/panda/protocol"
)

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

// KeepAlive 定时发送应用层心跳，调用方负责启动、取消并等待此方法退出。
func (c *Client) KeepAlive(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("heartbeat interval must be positive")
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return errors.New("connection closed")
		case <-ticker.C:
			heartbeat, cancel := context.WithTimeout(ctx, interval)
			err := c.Heartbeat(heartbeat)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}
