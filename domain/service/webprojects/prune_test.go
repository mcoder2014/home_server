package webprojects

import (
	"testing"

	"github.com/mcoder2014/home_server/domain/model"
	"github.com/stretchr/testify/require"
)

func TestSelectReleasesForPruningAggregatesInGoAndProtectsCurrentRelease(t *testing.T) {
	currentID := int64(3)
	releases := []*model.WebProjectRelease{
		{ID: 1, Status: model.WebProjectReleaseReady, TotalBytes: 10},
		{ID: 2, Status: model.WebProjectReleaseReady, TotalBytes: 20},
		{ID: currentID, Status: model.WebProjectReleaseReady, TotalBytes: 30},
	}

	retired, err := SelectReleasesForPruning(releases, &currentID, 100, 3, 70)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, releaseIDsForPruneTest(retired))
}

func TestSelectReleasesForPruningRejectsQuotaThatRequiresCurrentRelease(t *testing.T) {
	currentID := int64(1)
	releases := []*model.WebProjectRelease{
		{ID: currentID, Status: model.WebProjectReleaseReady, TotalBytes: 80},
	}

	_, err := SelectReleasesForPruning(releases, &currentID, 100, 2, 30)
	require.ErrorIs(t, err, ErrTooLarge)

	_, err = SelectReleasesForPruning(releases, &currentID, 100, 1, 1)
	require.ErrorIs(t, err, ErrRateLimited)
}

func releaseIDsForPruneTest(releases []*model.WebProjectRelease) []int64 {
	ids := make([]int64, 0, len(releases))
	for _, release := range releases {
		ids = append(ids, release.ID)
	}
	return ids
}
