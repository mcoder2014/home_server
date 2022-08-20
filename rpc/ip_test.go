package rpc

import (
	"context"
	"fmt"
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
