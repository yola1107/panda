// Package gate 管理客户端入口、连接生命周期和会话。
package gate

import (
	"errors"
	"time"

	"github.com/yola1107/panda/protocol"
)

// Config 为 Gate 提供实例标识和入口配置。
type Config struct {
	ID      string  `json:"id"`
	Entries []Entry `json:"entries"`
}

// Entry 将一个入口绑定到一个逻辑服务和 Codec。零限制使用默认值。
type Entry struct {
	Network          string        `json:"network"`
	Address          string        `json:"address"`
	Path             string        `json:"path"`
	Service          string        `json:"service"`
	Codec            string        `json:"codec"`
	LoginRoute       uint32        `json:"login_route"`
	MaxSize          int           `json:"max_size"`
	AuthTimeout      time.Duration `json:"auth_timeout"`
	IdleTimeout      time.Duration `json:"idle_timeout"`
	ExecutionTimeout time.Duration `json:"execution_timeout"`
	SendQueue        int           `json:"send_queue"`
	MaxInFlight      int           `json:"max_in_flight"`
}

func (e *Entry) defaults() error {
	if e.MaxSize == 0 {
		e.MaxSize = protocol.DefaultMaxSize
	}
	if e.AuthTimeout == 0 {
		e.AuthTimeout = 10 * time.Second
	}
	if e.IdleTimeout == 0 {
		e.IdleTimeout = 60 * time.Second
	}
	if e.ExecutionTimeout == 0 {
		e.ExecutionTimeout = 5 * time.Second
	}
	if e.SendQueue == 0 {
		e.SendQueue = 64
	}
	if e.MaxInFlight == 0 {
		e.MaxInFlight = 16
	}

	return e.validate()
}

func (e Entry) validate() error {
	if e.Network != "tcp" && e.Network != "websocket" {
		return errors.New("unsupported entry transport")
	}
	if e.Address == "" || e.Service == "" || e.Codec == "" || e.LoginRoute == 0 {
		return errors.New("entry requires address, service, codec and login route")
	}
	if e.Network == "websocket" && (len(e.Path) < 1 || e.Path[0] != '/') {
		return errors.New("WebSocket path must start with /")
	}

	for _, value := range []int64{
		int64(e.MaxSize - protocol.HeaderSize + 1),
		int64(e.AuthTimeout),
		int64(e.IdleTimeout),
		int64(e.ExecutionTimeout),
		int64(e.SendQueue),
		int64(e.MaxInFlight),
	} {
		if value < 1 {
			return errors.New("entry limits must be positive")
		}
	}

	return nil
}
