package dal

import (
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/model"
)

const (
	TableWebDAVLog = "webdav_log"
)

type WebDAVLogDao struct {
	*db.BaseDAO
}

func NewWebDAVLogDao() (*WebDAVLogDao, error) {
	dao, err := db.NewDAO(db.MasterDB(), TableWebDAVLog)
	if err != nil {
		return nil, err
	}
	return &WebDAVLogDao{
		BaseDAO: dao,
	}, nil
}

func (d *WebDAVLogDao) BatchCreate(records []*model.WebDAVLogEntity) error {
	if len(records) == 0 {
		return nil
	}
	return d.Table().Create(records).Error
}
