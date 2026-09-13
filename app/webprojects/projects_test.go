package webprojects

import (
	"testing"

	"github.com/mcoder2014/home_server/config"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/stretchr/testify/require"
)

func TestAcquireUploadReleasesBothUserAndGlobalCapacity(t *testing.T) {
	application := New(nil)
	conf := &config.WebProjectsConfig{Enabled: true, MaxConcurrentUploadsPerUser: 1, MaxConcurrentExtracts: 1}

	release, err := application.AcquireUpload(101, conf)
	require.NoError(t, err)
	_, err = application.AcquireUpload(101, conf)
	require.ErrorIs(t, err, service.ErrRateLimited)
	_, err = application.AcquireUpload(202, conf)
	require.ErrorIs(t, err, service.ErrRateLimited)

	release()
	secondRelease, err := application.AcquireUpload(202, conf)
	require.NoError(t, err)
	secondRelease()
}
