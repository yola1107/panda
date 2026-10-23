package remote

import (
	"context"
	"sync"
	"sync/atomic"

	internalv1 "github.com/yola1107/panda/api/panda/transport/v1"
	"github.com/yola1107/panda/internal/fault"

	"google.golang.org/grpc"
)

type backend struct {
	conn   *grpc.ClientConn
	client internalv1.NodeClient
	done   chan struct{}
	once   sync.Once
	ready  atomic.Bool
}

func (b *backend) Forward(ctx context.Context, req *internalv1.ForwardRequest) (*internalv1.ForwardReply, error) {
	reply, err := b.client.Forward(ctx, req)
	if err != nil {
		return nil, fault.Normalize(err)
	}

	b.ready.Store(true)

	return reply, nil
}

func (b *backend) Done() <-chan struct{} { return b.done }

func (b *backend) close() {
	b.once.Do(func() {
		close(b.done)
		_ = b.conn.Close()
	})
}
