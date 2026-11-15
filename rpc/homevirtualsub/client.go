package homevirtualsub

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mcoder2014/home_server/config"
	apperrors "github.com/mcoder2014/home_server/errors"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(c config.HomeVirtualSubConfig) (*Client, error) {
	if err := config.NormalizeHomeVirtualSub(&c); err != nil {
		return nil, err
	}
	if !c.Enabled {
		return nil, nil
	}
	info, err := os.Lstat(c.TokenFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 512 {
		return nil, fmt.Errorf("home virtual sub token_file must be a private regular file")
	}
	content, err := os.ReadFile(c.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read home virtual sub service credential")
	}
	token := strings.TrimSpace(string(content))
	if len(token) < 32 || len(token) > 256 || strings.IndexFunc(token, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
		return nil, fmt.Errorf("invalid home virtual sub service credential")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Client{baseURL: c.BaseURL, token: token, http: &http.Client{
		Timeout: time.Duration(c.TimeoutMS) * time.Millisecond, Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func ValidRevision(raw string) bool {
	if len(raw) != 64 || strings.ToLower(raw) != raw {
		return false
	}
	_, err := hex.DecodeString(raw)
	return err == nil
}

func (c *Client) Status(ctx context.Context) (*Status, error) {
	var result Status
	if err := c.request(ctx, http.MethodGet, "/status", "", "", nil, &result); err != nil {
		return nil, err
	}
	if result.APIVersion != 1 || !ValidRevision(result.Revision) {
		return nil, apperrors.ErrDependency
	}
	result.Enabled = true
	if result.CoreType != "xray" && result.CoreType != "v2ray" {
		result.CoreType = "unknown"
	}
	for i := range result.Workers {
		w := &result.Workers[i]
		if w.LastError != "" {
			w.LastError = "最近操作失败，请检查服务运行情况"
		}
		switch w.State {
		case "pending", "running", "stopped", "error":
		default:
			w.State = "unknown"
		}
	}
	return &result, nil
}

func (c *Client) List(ctx context.Context) (*SubscriptionList, error) {
	var result SubscriptionList
	if err := c.request(ctx, http.MethodGet, "/subscriptions", "", "", nil, &result); err != nil {
		return nil, err
	}
	if !ValidRevision(result.Revision) {
		return nil, apperrors.ErrDependency
	}
	return &result, nil
}

func (c *Client) Detail(ctx context.Context, name string) (*SubscriptionDetail, error) {
	if name == "" || len(name) > 256 {
		return nil, apperrors.ErrInvalid
	}
	var result SubscriptionDetail
	if err := c.request(ctx, http.MethodGet, "/subscriptions/detail", name, "", nil, &result); err != nil {
		return nil, err
	}
	if !ValidRevision(result.Revision) {
		return nil, apperrors.ErrDependency
	}
	return &result, nil
}

func (c *Client) Mutate(ctx context.Context, action, name, revision string, input SubscriptionInput) (*MutationResult, error) {
	method, path := http.MethodPost, "/subscriptions"
	switch action {
	case "create":
	case "update":
		method = http.MethodPatch
	case "delete":
		method = http.MethodDelete
	case "refresh", "start", "stop":
		path += "/" + action
	default:
		return nil, apperrors.ErrInvalid
	}
	if !ValidRevision(revision) || (action != "create" && (name == "" || len(name) > 256)) {
		return nil, apperrors.ErrInvalid
	}
	var result MutationResult
	if err := c.request(ctx, method, path, name, revision, input, &result); err != nil {
		return nil, err
	}
	if !ValidRevision(result.Revision) {
		return nil, apperrors.WithMessage(apperrors.ErrDependency, "操作结果未知，请刷新配置和状态核对后再操作")
	}
	switch result.ApplyState {
	case "pending":
		result.Message = "操作已接收，正在生效，请刷新状态核对"
	case "applied":
		result.Message = "操作已完成"
	case "error":
		result.Message = "配置已处理，但运行失败，请刷新状态核对"
	default:
		return nil, apperrors.WithMessage(apperrors.ErrDependency, "操作结果未知，请刷新配置和状态核对后再操作")
	}
	return &result, nil
}

// request uses only fixed paths, refuses redirects and never retries mutations.
// Caller-facing errors are generated here, without forwarding service text.
func (c *Client) request(ctx context.Context, method, path, name, revision string, input interface{}, result interface{}) error {
	if c == nil {
		return apperrors.ErrNotFound
	}
	address := c.baseURL + "/management/v1" + path
	if name != "" {
		address += "?" + url.Values{"name": {name}}.Encode()
	}
	var body io.Reader
	if input != nil {
		content, err := json.Marshal(input)
		if err != nil {
			return apperrors.ErrInvalid
		}
		body = bytes.NewReader(content)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return apperrors.ErrInvalid
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if revision != "" {
		req.Header.Set("If-Match", `"`+revision+`"`)
	}
	response, err := c.http.Do(req)
	if err != nil {
		if method != http.MethodGet {
			return apperrors.WithMessage(apperrors.ErrDependency, "连接中断，操作结果未知，请刷新配置和状态核对后再操作")
		}
		return apperrors.WithMessage(apperrors.ErrDependency, "home virtual sub 暂时无法连接，当前状态未知")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case http.StatusBadRequest:
			return apperrors.WithMessage(apperrors.ErrInvalid, "订阅字段或监听端口无效，请检查配置")
		case http.StatusConflict:
			return apperrors.WithMessage(apperrors.ErrConflict, "配置已变化或操作正在执行，请刷新后重新确认")
		case http.StatusNotFound:
			return apperrors.WithMessage(apperrors.ErrNotFound, "订阅不存在，请刷新配置")
		default:
			return apperrors.WithMessage(apperrors.ErrDependency, "home virtual sub 服务不可用，请核对连接与状态")
		}
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(content) > 1<<20 || json.Unmarshal(content, result) != nil {
		return apperrors.WithMessage(apperrors.ErrDependency, "服务响应无效，请刷新配置和状态核对后再操作")
	}
	return nil
}
