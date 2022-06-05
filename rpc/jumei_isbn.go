package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/mcoder2014/home_server/utils/log"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/errors"
)

/**
用于查询 isbn 的数据

curl -i -k -X POST 'https://jmisbn.market.alicloudapi.com/isbn/query' \
-H 'Authorization:APPCODE 你自己的AppCode' \
--data 'isbn=isbn'


*/

const (
	JumeiURL = "https://jmisbn.market.alicloudapi.com"
)

// JumeiResponse 聚美的基本返回结构
type JumeiResponse struct {
	Data struct {
		Details []*JumeiBookModel `json:"details"`
	} `json:"data"`

	Msg     string `json:"msg"`
	Success bool   `json:"success"`
	Code    int    `json:"code"`
	TaskNo  string `json:"taskNo"`
}

type JumeiBookModel struct {
	Series    string `json:"series"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	Publisher string `json:"publisher"`
	PubDate   string `json:"pubDate"`
	Isbn      string `json:"isbn"`
	Isbn10    string `json:"isbn10"`
	Price     string `json:"price"`
	Format    string `json:"format"`
	Binding   string `json:"binding"`
	Page      string `json:"page"`
	Img       string `json:"img"`
	Gist      string `json:"gist"`
}

func GetBookInfoByISBN(ctx context.Context, isbn string) (*model.BookInfo, error) {

	// 构造 request
	client := &http.Client{}
	var data = strings.NewReader(fmt.Sprintf(`isbn=%s`, isbn))
	req, err := http.NewRequest("POST", JumeiURL+"/isbn/query", data)
	if err != nil {
		return nil, errors.New(errors.ErrorCodeParamInvalid)
	}

	appCode := config.Global().RPC.JmIsbn.AppCode
	if len(appCode) == 0 {
		return nil, errors.NewWithMessage(errors.ErrorCodeRpcUnauthorized, "appcode is not config")
	}

	req.Header.Set("Authorization", "APPCODE "+appCode)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	// 执行 http 请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New(errors.ErrorCodeRpcFailed)
	}

	defer resp.Body.Close()

	// 读取查询结果
	bodyText, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.New(errors.ErrorCodeRpcFailed)
	}

	// 检查 statusCode
	if err = statusCodeCheck(resp); err != nil {
		log.Ctx(ctx).Warnf("Query isbn:%v Resp header: %+v\n Resp Body: %v", isbn, resp.Header, string(bodyText))
		return nil, err
	}

	// 打印每次收到的 body
	log.Ctx(ctx).Infof("get info %s", bodyText)

	jumeiModel := &JumeiResponse{}
	err = json.Unmarshal(bodyText, jumeiModel)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrorCodeRpcUnknownResponse,
			fmt.Sprintf("body: %v", bodyText))
	}

	if jumeiModel.Code != 200 {
		return nil, errors.New(errors.ErrorCodeRpcFailed)
	}
	if len(jumeiModel.Data.Details) == 0 {
		return nil, errors.New(errors.ErrorCodeBookNotFound)
	}

	return JumeiToModel(jumeiModel.Data.Details[0]), nil
}

func statusCodeCheck(resp *http.Response) error {
	if resp == nil {
		return errors.New(errors.ErrorCodeRpcFailed)
	}

	switch resp.StatusCode {
	case 200:
		return nil
	case 400:
		// 这里可能是传入的 isbn 错误，也可能是图书不存在
		return errors.New(errors.ErrorCodeBookNotFound)
	case 403:
		errorMessage := resp.Header.Get("x-ca-error-message")
		if errorMessage == "Quota Exhausted" {
			return errors.NewWithMessage(errors.ErrorCodeRpcNoQuota, fmt.Sprintf("header info: %+v", resp.Header))
		} else if errorMessage == "Unauthorized" {
			return errors.NewWithMessage(errors.ErrorCodeRpcUnauthorized, fmt.Sprintf("header info: %+v", resp.Header))
		}
	case 500:
		return errors.NewWithMessage(errors.ErrorCodeRpcServerError, "api supplier internal error, retry after few minutes")
	}
	return errors.NewWithMessage(errors.ErrorCodeUnknownError, fmt.Sprintf("Response Code: %v is not 200", resp.StatusCode))
}

func JumeiToModel(jumei *JumeiBookModel) *model.BookInfo {
	if jumei == nil {
		return nil
	}

	// 防止聚美的 page 有中文
	page, err := strconv.Atoi(jumei.Page)
	if err != nil {
		page = 0
	}

	return &model.BookInfo{
		Title:     jumei.Title,
		Author:    jumei.Author,
		Publisher: jumei.Publisher,
		//PubDate:   jumei.PubDate,
		Isbn:    jumei.Isbn,
		Isbn10:  jumei.Isbn10,
		Price:   jumei.Price,
		Page:    page,
		Img:     jumei.Img,
		Summary: jumei.Gist,
	}
}
