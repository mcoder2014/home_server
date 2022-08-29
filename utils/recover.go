package utils

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/mcoder2014/home_server/utils/log"
)

// Recovery 避免 panic
func Recovery(ctx context.Context) {
	e := recover()
	if e == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := fmt.Errorf("%v", e)

	log.Ctx(ctx).WithError(err).Errorf(
		"catch panic!!!\n stacktrace:\n%s", debug.Stack())
}
