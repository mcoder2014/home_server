package service

import (
	"context"
	"sync"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/rpc"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/log"
	"github.com/mcoder2014/home_server/utils/routine"
)

func QueryBookInfoByIsbn(ctx context.Context, isbn string) (*model.BookInfo, error) {
	info, err := dal.QueryBookInfoByIsbn(isbn)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrorCodeDbError)
	}
	if info != nil {
		log.Ctx(ctx).Infof("QueryBookInfoByIsbn got result from local database")
		return info, nil
	}

	return getBookInfoByRpc(ctx, isbn)
}

func getBookInfoByRpc(ctx context.Context, isbn string) (*model.BookInfo, error) {
	// 调用 rpc 查询
	info, err := rpc.GetBookInfoByISBN(ctx, isbn)
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

func BatchQueryBookInfo(ctx context.Context, isbnList []string) (map[string]*model.BookInfo, error) {
	bookinfoList, e := dal.BatchQueryBookInfoByIsbn(isbnList)
	if e != nil {
		return nil, errors.Wrap(e, errors.ErrorCodeDbError)
	}

	// 过滤出数据库中无缓存的结果
	isbnNotFoundList := getIsbnNotFoundList(isbnList, bookinfoList)
	if len(isbnNotFoundList) == 0 {
		return model.BookInfoSliceToMapByIsbnList(isbnList, bookinfoList), nil
	}

	log.Ctx(ctx).Infof("isbnNotFoundList: %v", isbnNotFoundList)

	// 调用 rpc 查询
	wg := sync.WaitGroup{}
	var resultCollector sync.Map
	for _, isbn := range isbnNotFoundList {
		wg.Add(1)
		queryIsbn := isbn
		routine.Go(func() {
			defer wg.Done()
			info, err := getBookInfoByRpc(ctx, queryIsbn)
			if err != nil {
				log.Ctx(ctx).WithError(err).Errorf("getBookInfoByRpc error, queryIsbn:%v", queryIsbn)
				return
			}
			resultCollector.Store(queryIsbn, info)
		})
	}
	wg.Wait()

	resultCollector.Range(func(key, value interface{}) (res bool) {
		info, ok := value.(*model.BookInfo)
		if !ok {
			log.Ctx(ctx).Errorf("getBookInfoByRpc resultCollector error, key: %+v,  value :%+v, break", key, value)
			return false
		}
		bookinfoList = append(bookinfoList, info)
		return true
	})
	return model.BookInfoSliceToMapByIsbnList(isbnList, bookinfoList), nil
}

func getIsbnNotFoundList(isbnList []string, data []*model.BookInfo) []string {
	isbnMap := utils.SliceToMapStr(isbnList)
	var isbnNotFoundList []string
	for _, b := range data {
		if b == nil {
			continue
		}
		isbnMap[b.Isbn10] = false
		isbnMap[b.Isbn] = false
	}
	for isbn, v := range isbnMap {
		if !v {
			continue
		}
		isbnNotFoundList = append(isbnNotFoundList, isbn)
	}
	return isbnNotFoundList
}
