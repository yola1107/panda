// Package transportio 统一 TCP 与 WebSocket 的有界读写。
package transportio

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/yola1107/panda/protocol"

	"github.com/gorilla/websocket"
)

// Connection 只允许一个读取者和一个写入者并行工作。
type Connection struct {
	tcp net.Conn
	ws  *websocket.Conn
}

// TCP 接管 conn 的关闭责任。
func TCP(conn net.Conn) *Connection { return &Connection{tcp: conn} }

// WebSocket 接管 conn 的关闭责任。
func WebSocket(conn *websocket.Conn) *Connection { return &Connection{ws: conn} }

// Dial 建立指定传输的连接，WebSocket 地址须包含路径。
func Dial(ctx context.Context, network, address string) (*Connection, error) {
	if network == "tcp" {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}

		return TCP(conn), nil
	}

	if network != "websocket" {
		return nil, errors.New("unsupported transport")
	}

	conn, response, err := websocket.DefaultDialer.DialContext(ctx, address, http.Header{})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}

	if err != nil {
		return nil, err
	}

	return WebSocket(conn), nil
}

// Read 读取一帧；长度限制在传输层和信封层均执行。
func (c *Connection) Read(maxSize int, deadline time.Time) (protocol.Message, error) {
	if c.tcp != nil {
		if err := c.tcp.SetReadDeadline(deadline); err != nil {
			return protocol.Message{}, err
		}

		return protocol.ReadTCP(c.tcp, maxSize)
	}

	if err := c.ws.SetReadDeadline(deadline); err != nil {
		return protocol.Message{}, err
	}

	c.ws.SetReadLimit(int64(maxSize))

	typ, reader, err := c.ws.NextReader()
	if err != nil {
		return protocol.Message{}, err
	}

	if typ != websocket.BinaryMessage {
		return protocol.Message{}, errors.New("binary message required")
	}

	return protocol.ReadWebSocket(reader, maxSize)
}

// Write 写入已经编码的信封。
func (c *Connection) Write(data []byte, deadline time.Time) error {
	if c.tcp != nil {
		if err := c.tcp.SetWriteDeadline(deadline); err != nil {
			return err
		}

		return protocol.WriteFrame(c.tcp, data)
	}

	if err := c.ws.SetWriteDeadline(deadline); err != nil {
		return err
	}

	return c.ws.WriteMessage(websocket.BinaryMessage, data)
}

// Close 中断阻塞的读写，允许重复调用。
func (c *Connection) Close() error {
	if c.tcp != nil {
		return c.tcp.Close()
	}

	return c.ws.Close()
}
