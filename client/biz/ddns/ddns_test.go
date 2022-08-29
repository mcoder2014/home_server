package ddns

import (
	"context"
	"testing"

	"github.com/mcoder2014/home_server/client/config"
	"github.com/mcoder2014/home_server/client/rpc"
	"github.com/mcoder2014/home_server/client/testutil"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestMain(t *testing.M) {
	_ = testutil.Init()
	t.Run()
}

func Test_createNewRecord(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		domainConfig *config.DomainConfig
		ipAddr       string
		wantErr      bool
	}{
		{
			name: "test",
			domainConfig: &config.DomainConfig{
				Domain:    "a.mcoder.cc",
				IPVersion: config.IPV4,
			},
			ipAddr:  "192.168.31.10",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := createNewRecord(ctx, tt.domainConfig, tt.ipAddr)
			require.Equalf(t, tt.wantErr, err != nil, "createNewRecord failed")
		})
	}
}

func TestUpdateRecord(t *testing.T) {
	ctx := context.Background()
	records, err := rpc.GetAllDNSRecord(context.Background(), config.Global().Cloudflare.Zone)
	require.Nil(t, err)
	recordMap := convertToDNSRecordMap(records)

	tests := []struct {
		name         string
		domainConfig *config.DomainConfig
		ipAddr       string
		wantErr      bool
	}{
		{
			name: "normal",
			domainConfig: &config.DomainConfig{
				Domain:    "a.mcoder.cc",
				IPVersion: config.IPV4,
			},
			ipAddr:  "115.206.152.5",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			record, ok := recordMap[tt.domainConfig.Domain]
			require.True(t, ok)

			err = updateNewRecord(ctx, tt.domainConfig, tt.ipAddr, *record)
			require.Equalf(t, tt.wantErr, err != nil, "createNewRecord failed")
		})
	}

}

func Test_getIp(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		domainConfig *config.DomainConfig
		wantErr      bool
	}{
		{
			name: "normal ipv4",
			domainConfig: &config.DomainConfig{
				IPVersion: "ipv4",
			},
		},
		{
			name: "normal ipv6",
			domainConfig: &config.DomainConfig{
				IPVersion: "ipv6",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIpAddress, err := getIp(ctx, tt.domainConfig)
			require.Equal(t, tt.wantErr, err != nil)
			require.NotEmpty(t, gotIpAddress)
			logrus.Infof("ip addr %v", gotIpAddress)
		})
	}
}

func Test_domainCheck(t *testing.T) {

	ctx := context.Background()
	records, err := rpc.GetAllDNSRecord(context.Background(), config.Global().Cloudflare.Zone)
	require.Nil(t, err)
	recordMap := convertToDNSRecordMap(records)

	tests := []struct {
		name         string
		domainConfig *config.DomainConfig
		wantErr      bool
	}{
		{
			name: "normal existed ipv4",
			domainConfig: &config.DomainConfig{
				Domain:    "a.mcoder.cc",
				IPVersion: "ipv4",
			},
		},
		{
			name: "normal not existed ipv4",
			domainConfig: &config.DomainConfig{
				Domain:    "a4.mcoder.cc",
				IPVersion: "ipv4",
			},
		},
		{
			name: "normal existed ipv6",
			domainConfig: &config.DomainConfig{
				Domain:    "aaaa.mcoder.cc",
				IPVersion: "ipv6",
			},
		},
		{
			name: "normal not existed ipv6",
			domainConfig: &config.DomainConfig{
				Domain:    "a6.mcoder.cc",
				IPVersion: "ipv6",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domainCheck(ctx, tt.domainConfig, recordMap)
			require.Equal(t, tt.wantErr, err != nil)
		})
	}
}
