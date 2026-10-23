package client

import (
	"context"
	"errors"
	"time"
)

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
