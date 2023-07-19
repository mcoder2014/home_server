package service

import (
	"context"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
)

func AddAddress(ctx context.Context, address *model.BookAddress) (int64, error) {
	return dal.InsertBookAddress(address)
}
