package client

import (
	"context"

	"github.com/yola1107/panda/codec"

	"google.golang.org/protobuf/proto"
)

// OnPush 注册回调。回调顺序执行，须响应 ctx 取消，不能在回调内调用 Close。
func (c *Client) OnPush(route uint32, callback func(context.Context, []byte) error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.callbacks[route] = callback
}

// Decode 按客户端入口的 Codec 解码，供生成的类型化推送分发使用。
func (c *Client) Decode(payload []byte, message proto.Message) error {
	encoding, err := codec.Lookup(c.config.Codec)
	if err != nil {
		return err
	}

	return encoding.Unmarshal(payload, message)
}

func (c *Client) dispatch(ctx context.Context) {
	defer close(c.pushDone)
	defer c.shutdown()

	for {
		select {
		case <-ctx.Done():
			return
		case message := <-c.pushes:
			c.mu.Lock()
			callback := c.callbacks[message.Route]
			c.mu.Unlock()

			if callback != nil {
				if err := callback(ctx, message.Payload); err != nil {
					return
				}
			}
		}
	}
}
