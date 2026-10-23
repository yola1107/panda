package echo

import (
	"fmt"

	"github.com/yola1107/panda/gate"
	"github.com/yola1107/panda/internal/staticdiscovery"

	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/file"
	"github.com/go-kratos/kratos/v3/registry"
)

// Config 通过 Kratos Config 加载；时间值单位为纳秒，不热更新。
type Config struct {
	Gate             gate.Config               `json:"gate"`
	NodeMaxExecuting int                       `json:"node_max_executing"`
	NodeRPC          string                    `json:"node_rpc"`
	GateRPC          string                    `json:"gate_rpc"`
	Discovery        staticdiscovery.Discovery `json:"discovery"`
}

// Load 加载强类型示例配置并关闭文件监听。
func Load(path string) (Config, error) {
	source := config.New(config.WithSource(file.NewSource(path)))
	defer func() { _ = source.Close() }()

	if err := source.Load(); err != nil {
		return Config{}, fmt.Errorf("load example config: %w", err)
	}

	var result Config
	if err := source.Scan(&result); err != nil {
		return Config{}, fmt.Errorf("decode example config: %w", err)
	}

	return result, nil
}

// Registry 提供可替换的 Kratos 服务发现接口。
func (c Config) Registry() registry.Discovery { return c.Discovery }
