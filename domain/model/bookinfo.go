package model

import "time"

type BookInfo struct {
	// 标题
	Title string `json:"title"`
	// 作者
	Author string `json:"author"`
	// 出版社
	Publisher string `json:"publisher"`
	// 出版时间
	PubDate time.Time `json:"pub_date"`
	// 13 位 ISBN 编码
	Isbn string `json:"isbn"`
	// 10 位 ISBN 编码
	Isbn10 string `json:"isbn_10"`
	// 定价
	Price string `json:"price"`
	// 页数
	Page int `json:"page"`
	// 封面图
	Img string `json:"img"`
	// 摘要
	Summary string `json:"summary"`
}
