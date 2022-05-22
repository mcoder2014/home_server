package model

import "time"

// StorageStatus 库存记录的状态
type StorageStatus int

const (
	// StorageStatusNormal 状态正常
	StorageStatusNormal StorageStatus = iota + 1
	// StorageStatusStop 已失效
	StorageStatusStop
)

func StorageStatusPtr(s StorageStatus) *StorageStatus {
	return &s
}

// StorageType 库存类型：自有、图书馆借阅、电子书等
type StorageType int

const (
	// StorageTypeOwn 自有
	StorageTypeOwn StorageType = iota + 1
	// StorageTypeEbook 电子书
	StorageTypeEbook
	// StorageTypeBBorrow 借阅
	StorageTypeBBorrow
)

func StorageTypePtr(s StorageType) *StorageType {
	return &s
}

type DBBookStorage struct {
	// 数据库主键
	Id int64 `json:"id" gorm:"column:id"`
	// 该条库存记录的状态
	Status StorageStatus `json:"status" gorm:"column:status"`
	// 库存类型
	Type StorageType `json:"type" gorm:"column:type"`
	// 关联的图书信息 ID
	BookId int64 `json:"book_id" gorm:"column:bid"`
	// 数量
	Quantity int `json:"quantity" gorm:"column:quantity"`
	// 13 位 ISBN 编号
	Isbn string `json:"isbn" gorm:"column:isbn13"`
	// 10 位 ISBN 编号
	Isbn10 string `json:"isbn10" gorm:"column:isbn10"`
	// 图书所在位置 id
	LibraryId int64 `json:"library_id" gorm:"column:libraryid"`
	// 拓展信息
	Extra string `json:"extra" gorm:"column:extra"`
	DalModel
}

// BookStorageExtra 用来存储一些特别的信息
type BookStorageExtra struct {
	// DownloadUrl 电子图书的下载地址
	DownloadUrl *string
}

type UpdateBookStorageDto struct {
	Id        int64
	Status    *StorageStatus
	Type      *StorageType
	BookId    *int64
	Quantity  *int
	Isbn      *string
	Isbn10    *string
	LibraryId *int64
}

func (d *UpdateBookStorageDto) ToFields() map[string]interface{} {
	res := map[string]interface{}{}
	if d.Status != nil {
		res["status"] = *(d.Status)
	}

	if d.Type != nil {
		res["type"] = *(d.Type)
	}

	if d.BookId != nil {
		res["bid"] = *(d.BookId)
	}

	if d.Quantity != nil {
		res["quantity"] = *(d.Quantity)
	}

	if d.Isbn != nil {
		res["isbn13"] = *(d.Isbn)
	}

	if d.Isbn10 != nil {
		res["isbn10"] = *(d.Isbn10)
	}

	if d.LibraryId != nil {
		res["libraryid"] = *(d.LibraryId)
	}

	if len(res) > 0 {
		res["update_time"] = time.Now()
	}

	return res
}

type BookStorage struct {
	// 数据库主键
	Id int64 `json:"id"`
	// 关联的图书信息 ID
	BookId int64 `json:"book_id"`
	// 图书所在位置 id
	LibraryId int64 `json:"library_id"`
	// 该条库存记录的状态
	Status StorageStatus `json:"status"`
	// 库存类型
	Type StorageType `json:"type"`
	// 数量
	Quantity int `json:"quantity"`
	// 13 位 ISBN 编号
	Isbn string `json:"isbn"`
	// 10 位 ISBN 编号
	Isbn10 string `json:"isbn10"`
	// 标题
	Title string `json:"title"`
	// 作者
	Author string `json:"author"`
	// 出版社
	Publisher string `json:"publisher"`
	// 出版时间
	PubDate time.Time `json:"pub_date"`
	// 定价
	Price string `json:"price"`
	// 页数
	Page int `json:"page"`
	// 封面图
	Img string `json:"img"`
	// 摘要
	Summary string `json:"summary"`
	// 地址简称
	AddressShortName string `json:"address_short_name"`

	DalModel
}

func GetBookStorage(info *BookInfo, storage *DBBookStorage, address *BookAddress) *BookStorage {
	s := &BookStorage{}
	if info != nil {
		s.BookId = info.Id
		s.Isbn = info.Isbn
		s.Isbn10 = info.Isbn10
		s.Title = info.Title
		s.Author = info.Author
		s.Publisher = info.Publisher
		s.PubDate = info.PubDate
		s.Img = info.Img
		s.Summary = info.Summary
	}

	if address != nil {
		s.LibraryId = address.Id
		s.AddressShortName = address.ShortName
	}

	if storage != nil {
		s.Id = storage.Id
		s.BookId = storage.BookId
		s.Quantity = storage.Quantity
		s.CreateTime = storage.CreateTime
		s.UpdateTime = storage.UpdateTime
		s.Status = storage.Status
		s.Type = storage.Type
		s.LibraryId = storage.LibraryId
	}
	return s
}
