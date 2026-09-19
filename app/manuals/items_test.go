package manuals

import (
	"testing"

	"github.com/mcoder2014/home_server/config"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/stretchr/testify/require"
)

func TestManualUploadReservationPreventsConcurrentDiskOvercommit(t *testing.T) {
	application := New()
	application.diskFree = func(string) (uint64, error) {
		return 200, nil
	}
	conf := config.ManualsConfig{Enabled: true, StorageRoot: "/unused", MaxFileBytes: 100, MinFreeDiskBytes: 10, PDFPreviewOutputLimitBytes: 50}

	release, err := application.AcquireUpload(&conf)
	require.NoError(t, err)
	_, err = application.AcquireUpload(&conf)
	require.ErrorIs(t, err, apperrors.ErrRateLimited)
	release()
	release()

	secondRelease, err := application.AcquireUpload(&conf)
	require.NoError(t, err)
	secondRelease()
}
