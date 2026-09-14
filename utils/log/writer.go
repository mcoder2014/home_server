package log

import (
	"fmt"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

type MyLogWriter struct {
	Logger *lumberjack.Logger
	// ToStd 同时写入到标准输出和文件
	ToStd bool
}

// Write 按配置先写标准输出，再交给滚动文件写入器；标准输出写入失败时立即返回错误。
func (l *MyLogWriter) Write(p []byte) (n int, err error) {
	if l == nil {
		panic(fmt.Errorf("MyLogWriter is not initialized"))
	}

	if l.ToStd {
		content := p
		for _idx := 0; _idx < 1e3; _idx++ {
			n, err = os.Stdout.Write(content)
			if n == 0 || err != nil {
				break
			}
			content = content[n:]
		}
		if err != nil {
			return 0, err
		}
	}

	if l.Logger != nil {
		return l.Logger.Write(p)
	}
	return 0, nil
}

func (l *MyLogWriter) Close() error {
	if l == nil || l.Logger == nil {
		return nil
	}
	return l.Logger.Close()
}
