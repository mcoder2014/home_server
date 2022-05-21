package rpc

import (
	"context"
	"testing"

	"github.com/mcoder2014/home_server/utils/testutil"
)

func TestMain(t *testing.M) {
	_ = testutil.Init()
	t.Run()
}

func TestGetBookInfoByISBN(t *testing.T) {

	ctx := context.Background()

	tests := []struct {
		name    string
		isbn    string
		want    bool
		wantErr bool
	}{
		{
			name:    "success spring boot 编程思想",
			isbn:    "9787121360398",
			want:    true,
			wantErr: false,
		},
		{
			name:    "want failed",
			isbn:    "12",
			want:    false,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetBookInfoByISBN(ctx, tt.isbn)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetBookInfoByISBN() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if (got != nil) != tt.want {
				t.Errorf("Get book Failed.")
			}
		})
	}
}
