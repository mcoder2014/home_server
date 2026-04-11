package rpc

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	myErrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils/log"
	"github.com/pkg/errors"
)

var ipv4Services = []string{
	"https://ip.mcoder.cc",
	"https://4.ipw.cn",
	"https://api4.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://v4.ident.me",
}

var ipv6Services = []string{
	"https://6.ipw.cn",
	"https://api6.ipify.org",
	"https://ipv6.icanhazip.com",
	"https://v6.ident.me",
}

func getIpAddress(ctx context.Context, url string) (string, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

func getIpAddressWithFallback(ctx context.Context, urls []string) (string, error) {
	var lastErr error
	for _, url := range urls {
		ip, err := getIpAddress(ctx, url)
		if err == nil {
			return strings.TrimSpace(ip), nil
		}
		log.Ctx(ctx).Warnf("ip query service %v failed: %v, trying next", url, err)
		lastErr = err
	}
	return "", fmt.Errorf("all ip query services failed, last error: %w", lastErr)
}

func GetDefaultIpv4(ctx context.Context) (string, error) {
	return getIpAddressWithFallback(ctx, ipv4Services)
}

func GetDefaultIpv6(ctx context.Context) (string, error) {
	return getIpAddressWithFallback(ctx, ipv6Services)
}
