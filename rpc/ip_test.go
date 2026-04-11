package rpc

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/mcoder2014/home_server/utils/log"
	"github.com/stretchr/testify/require"
)

func TestGetDefaultIpv4(t *testing.T) {
	ctx := log.GetCtxWithLogID(context.Background())
	ipv4, err := GetDefaultIpv4(ctx)
	require.Nil(t, err)
	fmt.Printf("ipv4: %v\n", ipv4)

	ipv6, err := GetDefaultIpv6(ctx)
	require.Nil(t, err)
	fmt.Printf("ipv6: %v\n", ipv6)
}

func TestGetIpAddressWithFallback_AllFail(t *testing.T) {
	ctx := log.GetCtxWithLogID(context.Background())
	_, err := getIpAddressWithFallback(ctx, []string{
		"https://invalid.example.invalid",
		"https://another.invalid.example",
	})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "all ip query services failed")
}

func TestGetIpAddressWithFallback_FirstFail(t *testing.T) {
	ctx := log.GetCtxWithLogID(context.Background())
	ip, err := getIpAddressWithFallback(ctx, []string{
		"https://invalid.example.invalid",
		"https://ipv4.icanhazip.com",
	})
	require.Nil(t, err)
	require.Regexp(t, regexp.MustCompile(`\d+\.\d+\.\d+\.\d+`), ip)
	fmt.Printf("ip from fallback: %v\n", ip)
}

func TestIpv4Services_SelfHostedFirst(t *testing.T) {
	require.True(t, len(ipv4Services) > 0, "ipv4Services should not be empty")
	require.Equal(t, "https://ip.mcoder.cc", ipv4Services[0],
		"self-hosted IP service should be the first entry in ipv4Services")
}

func TestGetDefaultIpv4_Fallback(t *testing.T) {
	ctx := log.GetCtxWithLogID(context.Background())
	ip, err := GetDefaultIpv4(ctx)
	require.Nil(t, err)
	require.Equal(t, ip, strings.TrimSpace(ip), "IP should not have leading/trailing whitespace")
	require.Regexp(t, regexp.MustCompile(`\d+\.\d+\.\d+\.\d+`), ip)
	fmt.Printf("ipv4: %v\n", ip)
}
