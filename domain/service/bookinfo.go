package service

import (
	"context"

	"github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/rpc"
	"github.com/mcoder2014/home_server/utils/log"

	"github.com/mcoder2014/home_server/domain/dal"

	"github.com/mcoder2014/home_server/domain/model"
)

func QueryBookInfoByIsbn(ctx context.Context, isbn string) (*model.BookInfo, error) {
	info, err := dal.QueryBookInfoByIsbn(isbn)
	if err != nil {
		return nil, err
	}
	if info != nil {
		log.Ctx(ctx).Infof("QueryBookInfoByIsbn got result from local database")
		return info, nil
	}

	// 调用 rpc 查询
	info, err = rpc.GetBookInfoByISBN(ctx, isbn)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, errors.New(errors.ErrorCodeBookNotFound)
	}

	err = dal.InsertBookInfo(info)
	if err != nil {
		log.Ctx(ctx).WithError(err).Errorf("Backup Bookinfo failed.")
	}
	return info, nil
}
