package service

import (
	"context"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
)

func QueryEbookByPath(ctx context.Context, dir, filename string) (*model.BookStorage, error) {
	dbStorages, err := dal.QueryBookStorageByDirAndFilename(dir, filename)
	if err != nil {
		return nil, err
	}
	if len(dbStorages) == 0 {
		return nil, nil
	}
	dbStorage := dbStorages[0]
	bookinfo, err := QueryBookInfoByID(ctx, dbStorage.BookId)
	if err != nil {
		return nil, err
	}
	return model.GetBookStorage(bookinfo, dbStorage, nil), nil
}
