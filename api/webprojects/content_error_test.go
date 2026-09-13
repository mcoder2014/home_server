package webprojects

import (
	"errors"
	"net/http"
	"os"
	"syscall"
	"testing"

	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/stretchr/testify/require"
)

func TestContentFailureStatusKeepsInvalidAndMissingAssetsHidden(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		isEntry   bool
		isRegular bool
		want      int
	}{
		{name: "invalid path", err: service.ErrInvalid, want: http.StatusNotFound},
		{name: "missing asset", err: os.ErrNotExist, want: http.StatusNotFound},
		{name: "file used as asset directory", err: syscall.ENOTDIR, want: http.StatusNotFound},
		{name: "asset is a directory", isRegular: false, want: http.StatusNotFound},
		{name: "regular asset", isRegular: true, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, contentFailureStatus(tt.err, tt.isEntry, tt.isRegular))
		})
	}
}

func TestContentFailureStatusTreatsEntryAndStorageFailuresAsUnavailable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		isRegular bool
	}{
		{name: "entry missing", err: os.ErrNotExist},
		{name: "file used as entry directory", err: syscall.ENOTDIR},
		{name: "entry is a directory", isRegular: false},
		{name: "content root unavailable", err: service.ErrDependency},
		{name: "file stat failed", err: errors.New("forced stat failure")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, http.StatusServiceUnavailable, contentFailureStatus(tt.err, true, tt.isRegular))
		})
	}
}
