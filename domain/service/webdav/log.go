package webdav

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/utils/log"
	"golang.org/x/time/rate"
)

type LogRoutine struct {
	content chan *model.WebDAVLogEntity
	dao     *dal.WebDAVLogDao
	limiter *rate.Limiter
}

var defaultLogRoutine *LogRoutine

const logQPS = 100

func InitLogRoutine() error {
	dao, err := dal.NewWebDAVLogDao()
	if err != nil {
		return err
	}

	defaultLogRoutine = &LogRoutine{
		content: make(chan *model.WebDAVLogEntity, 10_0000),
		dao:     dao,
		limiter: rate.NewLimiter(rate.Limit(logQPS), logQPS),
	}
	go defaultLogRoutine.Routine()
	return nil
}

func SendLogEvent(logEntity *model.WebDAVLogEntity) error {
	if logEntity == nil {
		return nil
	}
	if defaultLogRoutine == nil {
		return fmt.Errorf("logRoutine is no init")
	}
	defaultLogRoutine.content <- logEntity
	return nil
}

func (r *LogRoutine) Routine() {
	if r == nil {
		panic("LogRoutine is nil")
	}
	// 监听 ctrl c 信号
	exitChan := make(chan os.Signal)
	signal.Notify(exitChan, os.Interrupt, os.Kill, syscall.SIGTERM)

	for {
		select {
		case <-exitChan:
			break
		case logEntity := <-r.content:
			if logEntity == nil {
				continue
			}

			_ = r.limiter.Wait(context.Background())
			err := r.dao.BatchCreate([]*model.WebDAVLogEntity{
				logEntity,
			})
			if err != nil {
				log.Ctx(context.Background()).Errorf("logID:%v LogRoutine: BatchCreate failed, err: %s", logEntity.LogID, err.Error())
			}
		}
	}
}
