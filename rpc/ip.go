package rpc

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"

	myErrors "github.com/mcoder2014/home_server/errors"
	"github.com/pkg/errors"
)

func getIpAddress(ctx context.Context, url string) (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", errors.Wrap(err, "create http request failed")
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "http request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.New(fmt.Sprintf("resp.Status Code is %v", resp.StatusCode))
	}

	// 读取查询结果
	bodyText, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", myErrors.New(myErrors.ErrorCodeRpcFailed)
	}

	return string(bodyText), nil
}

func GetDefaultIpv4(ctx context.Context) (string, error) {
	return getIpAddress(ctx, "https://4.ipw.cn")
}

func GetDefaultIpv6(ctx context.Context) (string, error) {
	return getIpAddress(ctx, "https://6.ipw.cn")

}
