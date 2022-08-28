package rpc

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetAllDNSRecord(t *testing.T) {
	records, err := GetAllDNSRecord(context.Background(), "mcoder.cc")
	require.Nil(t, err)
	fmt.Printf("records: \t%+v\n", records)
}
