package service

import (
	"context"
	"testing"

	"github.com/mcoder2014/home_server/utils/testutil"
	"github.com/sirupsen/logrus"

	"github.com/stretchr/testify/require"
)

func TestMain(t *testing.M) {
	e := testutil.Init()
	if e != nil {
		logrus.Errorf("e:%v", e)
	}
	t.Run()
}

func TestBatchQueryBookInfo(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		isbnList []string
		wantErr  bool
	}{

		{
			name:     "normal isbn",
			isbnList: []string{"9787121291609", "9787115546081"},
			wantErr:  false,
		},
		{
			name:     "normal isbn10",
			isbnList: []string{"7115546088"},
			wantErr:  false,
		},
		{
			name:     "mix isbn10 isbn",
			isbnList: []string{"9787121291609", "7115546088"},
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BatchQueryBookInfo(ctx, tt.isbnList)
			if (err != nil) != tt.wantErr {
				t.Errorf("BatchQueryBookInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			require.Equal(t, len(tt.isbnList), len(got))
		})
	}
}
