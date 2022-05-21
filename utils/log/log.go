package log

import (
	"context"

	"github.com/sirupsen/logrus"
)

const LogIDKey = "log_id"

func Ctx(ctx context.Context) *logrus.Entry {
	logger := logrus.New()

	fields := logrus.Fields{}
	// logID
	if c := ctx.Value(LogIDKey); c != nil {
		fields[LogIDKey] = c
	}

	return logger.WithFields(fields)
}
