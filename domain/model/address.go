package model

type BookAddress struct {
	// 数据库主键
	Id int64 `json:"id" gorm:"column:id"`
	// 详细地址
	Address string `json:"address" gorm:"column:address"`
	// 简称
	ShortName string `json:"short_name" gorm:"column:short_name"`
	DalModel
}

func SliceToMapBookAddress(address []*BookAddress) map[int64]*BookAddress {
	var result = make(map[int64]*BookAddress, len(address))
	for _, a := range address {
		if a == nil {
			continue
		}
		result[a.Id] = a
	}
	return result
}
