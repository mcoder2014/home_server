package log

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func GenLogID() string {
	currentTime := time.Now()
	uuidValue, err := uuid.NewUUID()
	if err != nil {
		return fmt.Sprintf("%v", currentTime.UnixNano())
	}

	return fmt.Sprintf("%v-%v", currentTime.Unix(), uuidValue.String())
}

func GetCtxWithLogID(ctx context.Context) context.Context {
	if val := ctx.Value(LogIDKey); val != nil {
		return ctx
	}
	ctx = context.WithValue(ctx, LogIDKey, GenLogID())
	return ctx
}
