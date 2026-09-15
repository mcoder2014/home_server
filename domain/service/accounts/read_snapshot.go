package accounts

import (
	"context"
	"sync"

	"github.com/mcoder2014/home_server/domain/model"
)

type readSnapshotKey struct{}
type requestReadSnapshot struct {
	sync.Mutex
	users   map[int64]*model.UserAccount
	modules map[string]bool
}

// WithReadSnapshot is only installed on GET/HEAD requests. It shares source
// reads within that request, never across requests or through a write transaction.
func WithReadSnapshot(ctx context.Context) context.Context {
	if _, ok := ctx.Value(readSnapshotKey{}).(*requestReadSnapshot); ok {
		return ctx
	}
	snapshot := &requestReadSnapshot{
		users:   make(map[int64]*model.UserAccount),
		modules: make(map[string]bool),
	}
	return context.WithValue(ctx, readSnapshotKey{}, snapshot)
}
