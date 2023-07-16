package log

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

const LogIDKey = "log_id"

var defaultLogOutput io.Writer
var defaultLogOutputOnce sync.Once

func Ctx(ctx context.Context) *logrus.Entry {
	logger := logrus.StandardLogger()
	fields := logrus.Fields{}
	// logID
	if c := ctx.Value(LogIDKey); c != nil {
		fields[LogIDKey] = c
	}

	return logger.WithFields(fields)
}

func Init() error {
	// logrus init
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})

	logrus.SetOutput(GetDefaultOutput())
	return nil
}

func GetDefaultOutput() io.Writer {
	defaultLogOutputOnce.Do(func() {
		l := &lumberjack.Logger{
			Filename:   "/var/log/home_server/run.log",
			MaxSize:    100, // megabytes
			MaxBackups: 64,
			MaxAge:     15,    //days
			Compress:   false, // disabled by default
		}

		myLogWriter := &MyLogWriter{
			Logger: l,
			ToStd:  true,
		}
		defaultLogOutput = myLogWriter

		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGHUP)

		go func() {
			for {
				<-c
				err := l.Rotate()
				if err != nil {
					logrus.Errorf("log rotate error: %v", err)
				}
			}
		}()
	})
	return defaultLogOutput
}
