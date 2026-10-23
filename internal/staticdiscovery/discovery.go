// Package staticdiscovery 为示例和端到端测试提供静态 Kratos 服务发现。
package staticdiscovery

import (
	"context"
	"sync"

	"github.com/go-kratos/kratos/v3/registry"
)

// Discovery 的实例表在创建后不可修改；生产环境应注入实际的注册中心。
type Discovery map[string][]*registry.ServiceInstance

// GetService 返回配置中的服务实例。
func (d Discovery) GetService(ctx context.Context, name string) ([]*registry.ServiceInstance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return d[name], nil
}

// Watch 先返回静态快照，再等待取消。
func (d Discovery) Watch(ctx context.Context, name string) (registry.Watcher, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	w := &watcher{
		values: make(chan []*registry.ServiceInstance, 1),
		done:   make(chan struct{}),
		parent: ctx.Done(),
	}
	w.values <- d[name]
	return w, nil
}

type watcher struct {
	values chan []*registry.ServiceInstance
	done   chan struct{}
	once   sync.Once
	parent <-chan struct{}
}

func (w *watcher) Next() ([]*registry.ServiceInstance, error) {
	select {
	case instances := <-w.values:
		return instances, nil
	case <-w.done:
		return nil, context.Canceled
	case <-w.parent:
		return nil, context.Canceled
	}
}

func (w *watcher) Stop() error {
	w.once.Do(func() { close(w.done) })
	return nil
}
