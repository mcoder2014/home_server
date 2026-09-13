package service

import (
	"context"

	"github.com/mcoder2014/home_server/utils"

	"github.com/mcoder2014/home_server/errors"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	"gorm.io/gorm"
)

// QueryStorageByIsbn 根据 ISBN 查库存，再补充书目信息和库位；不存在库存时不继续查询关联信息。
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

// AddStorageByIsbn 取得书目信息后新增库存；数据库身份模式下在写入事务内重新验证图书写权限。
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
	if accounts.DatabaseMode() {
		principal, _ := ctx.Value(utils.CtxKeyPrincipal).(*utils.Principal)
		e = db.MasterDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := accounts.RequireLibraryWriteTx(tx, principal); err != nil {
				return err
			}
			return dal.InsertBookStorage(&s, tx)
		})
	} else {
		e = dal.InsertBookStorage(&s)
	}
	return e
}

func UpdateStorage(ctx context.Context, dto *model.UpdateBookStorageDto) error {
	return dal.UpdateBookStorage(dto)
}

func AddAddress(ctx context.Context, address *model.BookAddress) (int64, error) {
	if accounts.DatabaseMode() {
		principal, _ := ctx.Value(utils.CtxKeyPrincipal).(*utils.Principal)
		var id int64
		err := db.MasterDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := accounts.RequireLibraryWriteTx(tx, principal); err != nil {
				return err
			}
			var err error
			id, err = dal.InsertBookAddress(address, tx)
			return err
		})
		return id, err
	}
	return dal.InsertBookAddress(address)
}

// GetTotalStorage 分页读取库存，批量补全书目和库位，并过滤关联信息不完整的记录。
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
