package service

import (
	"context"

	"github.com/mcoder2014/home_server/utils"

	"github.com/mcoder2014/home_server/errors"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
)

// QueryStorageByIsbn 根据 isbn 查询库存
func QueryStorageByIsbn(ctx context.Context, isbn string) (*model.BookStorage, error) {
	s, e := dal.QueryBookStorageByIsbn(isbn)
	if e != nil || s == nil {
		return nil, e
	}

	info, e := QueryBookInfoByIsbn(ctx, isbn)
	if e != nil {
		return nil, e
	}

	address, e := dal.QueryBookAddressById(s.LibraryId)
	if e != nil {
		return nil, e
	}

	bookStorage := model.GetBookStorage(info, s, address)
	return bookStorage, nil
}

func AddStorageByIsbn(ctx context.Context, isbn string, quantity int, t model.StorageType, libId int64) error {
	info, e := QueryBookInfoByIsbn(ctx, isbn)
	if e != nil || info == nil {
		return errors.New(errors.ErrorCodeBookNotFound)
	}

	s := model.DBBookStorage{
		BookId:    info.Id,
		LibraryId: libId,
		Isbn:      info.Isbn,
		Isbn10:    info.Isbn10,
		Status:    model.StorageStatusNormal,
		Type:      t,
		Quantity:  quantity,
	}
	e = dal.InsertBookStorage(&s)
	return e
}

func UpdateStorage(ctx context.Context, dto *model.UpdateBookStorageDto) error {
	return dal.UpdateBookStorage(dto)
}

func AddAddress(ctx context.Context, address *model.BookAddress) (int64, error) {
	return dal.InsertBookAddress(address)
}

// GetTotalStorage 分页查询全部图书
func GetTotalStorage(ctx context.Context, offset int, limit int) ([]*model.BookStorage, error) {

	// 查询库存
	dbs, err := dal.GetAllBookStorageOrderByUpdateTime(offset, limit)
	if err != nil {
		return nil, err
	}
	if len(dbs) == 0 {
		return nil, nil
	}
	var isbnList []string
	for _, s := range dbs {
		if s == nil {
			continue
		}
		isbnList = append(isbnList, s.Isbn)
	}
	// 查询图书信息
	bookinfos, e := BatchQueryBookInfo(ctx, isbnList)
	if e != nil {
		return nil, e
	}

	// 查询地址信息
	addressIDMap := make(map[int64]bool, len(dbs))
	for _, s := range dbs {
		addressIDMap[s.LibraryId] = true
	}
	addressList, e := dal.BatchQueryBookAddress(utils.MapToSliceInt64(addressIDMap))

	return model.BatchConvertBookStorage(dbs, bookinfos, addressList), nil
}
