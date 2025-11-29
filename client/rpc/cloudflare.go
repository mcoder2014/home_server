package rpc

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go"
	"github.com/mcoder2014/home_server/client/config"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/proxy"
)

func getApi() (*cloudflare.API, error) {
	if config.Global().Cloudflare == nil {
		return nil, fmt.Errorf("cloudflare config is nil")
	}
	apiToken := config.Global().Cloudflare.APIToken
	var opts []cloudflare.Option
	proxyStr := config.Global().Cloudflare.Proxy // 格式 "127.0.0.1:1080"
	if proxyStr != "" {
		// 1. 创建一个 SOCKS5 proxy
		dialer, err := proxy.SOCKS5("tcp", proxyStr, nil, proxy.Direct)
		if err != nil {
			log.Fatalf("Failed to create SOCKS5 dialer: %v", err)
		}

		// 2. 将 proxy 转换为 DialContext 类型，以便在 http.Transport 中使用
		dialContext, ok := dialer.(proxy.ContextDialer)
		if !ok {
			log.Fatal("SOCKS5 dialer does not implement ContextDialer")
		}

		// 3. 创建一个自定义的 http.Transport，并设置其 DialContext
		transport := &http.Transport{
			DialContext: dialContext.DialContext,
			// 你可以根据需要设置其他 Transport 字段，例如 TLSClientConfig, MaxIdleConns 等
			// 注意：不要在这里设置 Proxy 字段，因为我们已经用 DialContext 接管了连接
		}

		// 4. 使用自定义的 Transport 创建 http.Client
		httpClient := &http.Client{
			Transport: transport,
			Timeout:   20 * time.Second, // 设置一个超时时间
		}
		opts = append(opts, cloudflare.HTTPClient(httpClient))
		logrus.Infof("get cloudflare api using proxy=%s", proxyStr)
	}

	api, err := cloudflare.NewWithAPIToken(apiToken, opts...)
	if err != nil {
		return nil, err
	}
	api.Debug = config.Global().Cloudflare.Debug
	return api, nil
}

func GetAllDNSRecord(ctx context.Context, zone string) ([]cloudflare.DNSRecord, error) {
	api, err := getApi()
	if err != nil {
		return nil, err
	}

	//// Fetch the zone ID for zone example.org
	//zoneID, err := api.ZoneIDByName(zone)
	//if err != nil {
	//	return nil, err
	//}

	// Fetch all DNS records for example.org
	records, err := api.DNSRecords(context.Background(), zone, cloudflare.DNSRecord{})
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	for _, record := range records {
		sb.WriteString("[domain=")
		sb.WriteString(record.Name)
		sb.WriteString(" type=")
		sb.WriteString(record.Type)
		sb.WriteString(" content=")
		sb.WriteString(record.Content)
		sb.WriteString("],\t")
	}
	logrus.Infof("get ddns configs: len(%d), content=%s", len(records), sb.String())

	return records, nil
}

func CreateRecord(ctx context.Context, zone string, record cloudflare.DNSRecord) error {
	api, err := getApi()
	if err != nil {
		return err
	}

	resp, err := api.CreateDNSRecord(ctx, zone, record)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("create dns record failed: %+v", resp)
	}
	return nil
}

func UpdateRecord(ctx context.Context, zone string, recordID string, record cloudflare.DNSRecord) error {
	api, err := getApi()
	if err != nil {
		return err
	}
	err = api.UpdateDNSRecord(ctx, zone, recordID, record)
	return err
}
