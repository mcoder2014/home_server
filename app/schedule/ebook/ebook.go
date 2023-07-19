package ebook

import (
	"context"
	"fmt"
	"strings"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service"
	"github.com/mcoder2014/home_server/utils/log"
)

func Init() error {
	conf := config.Global()
	ebookConfig := conf.Routine.EBook
	if len(ebookConfig.ScanPaths) == 0 {
		return nil
	}
	go func() {
		ctx := log.GetCtxWithLogID(context.Background())
		log.Ctx(ctx).Infof("ebook config:%v", ebookConfig)
		s := &FileScanner{
			ScanPaths:      ebookConfig.ScanPaths,
			FilePostfixes:  ebookConfig.FilePostfixes,
			ExcludeRegexps: ebookConfig.ExcludeRegexps,
			Fns: []FileScannerCallback{
				func(ctx context.Context, dir, filepath string) error {
					log.Ctx(ctx).Infof("dir:%v, file:%v", dir, filepath)
					return nil
				},
				checkPDF,
			},
			SkipError: ebookConfig.SkipError,
		}
		err := s.Scan(ctx)
		if err != nil {
			log.Ctx(ctx).WithError(err).Errorf("meet error")
		}
		for _, e := range s.Errors {
			log.Ctx(ctx).WithError(e.Error).Errorf("dir:%v file:%v meet error", e.Dir, e.FileName)
		}
	}()
	return nil
}

func checkPDF(ctx context.Context, dir, filename string) error {
	record, err := service.QueryEbookByPath(ctx, dir, filename)
	if err != nil {
		return fmt.Errorf("checkPDF failed, %w", err)
	}
	if record != nil {
		log.Ctx(ctx).Infof("dir:%v file:%v already has record", dir, filename)
		return nil
	}

	// 添加记录
	bookInfo := genBookInfo(dir, filename)
	err = service.CreateBookInfo(ctx, bookInfo)
	if err != nil {
		return fmt.Errorf("create bookinfo failed :%w", err)
	}
	bookStorage := &model.DBBookStorage{
		Status:   model.StorageStatusNormal,
		Type:     model.StorageTypeEbook,
		BookId:   bookInfo.Id,
		Quantity: 1,
		Filename: filename,
		DirPath:  dir,
	}
	err = service.CreateBookStorage(ctx, bookStorage)
	return err
}

func genBookInfo(dir, filename string) *model.BookInfo {
	dotIdx := strings.LastIndex(filename, ".")
	title := filename[:dotIdx]
	return &model.BookInfo{
		Title:   title,
		Author:  "home_server",
		Summary: fmt.Sprintf("auto inserted by home_server, dirpath:%v filename:%v", dir, filename),
	}
}
