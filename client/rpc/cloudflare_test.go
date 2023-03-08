package rpc

import (
	"context"
	"fmt"
	"testing"

	"github.com/mcoder2014/home_server/client/testutil"
	"github.com/stretchr/testify/require"
)

func TestMain(t *testing.M) {
	_ = testutil.Init()
	t.Run()
}

func TestGetAllDNSRecord(t *testing.T) {
	records, err := GetAllDNSRecord(context.Background(), "50bb5112d172dc847c194c6b2e3fdcac")
	require.Nil(t, err)
	fmt.Printf("records: \t%+v\n", records)
}
