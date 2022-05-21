package model

import "time"

type BookInfo struct {

	// 主键
	Id int64 `json:"id" gorm:"column:id"`
	// 标题
	Title string `json:"title" gorm:"column:title"`
	// 作者
	Author string `json:"author" gorm:"column:author"`
	// 出版社
	Publisher string `json:"publisher" gorm:"column:publisher"`
	// 出版时间
	PubDate time.Time `json:"pub_date" gorm:"column:pubdate"`
	// 13 位 ISBN 编码
	Isbn string `json:"isbn" gorm:"column:isbn13"`
	// 10 位 ISBN 编码
	Isbn10 string `json:"isbn_10" gorm:"column:isbn10"`
	// 定价
	Price string `json:"price" gorm:"column:price"`
	// 页数
	Page int `json:"page" gorm:"column:pages"`
	// 封面图
	Img string `json:"img" gorm:"column:image"`
	// 摘要
	Summary string `json:"summary" gorm:"column:summary"`

	// 数据库通用字段
	DalModel
}
