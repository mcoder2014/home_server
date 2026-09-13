package applications

import (
	"testing"

	"github.com/go-sql-driver/mysql"
	appErrors "github.com/mcoder2014/home_server/errors"
	"github.com/stretchr/testify/require"
)

func TestAllocateApplicationSlotCountsLegacySlotsAboveNewLimit(t *testing.T) {
	slots := []int{11, 12, 13, 14, 15, 16, 17, 18, 19, 20}

	slot, err := allocateApplicationSlot(slots, 10)

	require.ErrorIs(t, err, appErrors.ErrRateLimited)
	require.Zero(t, slot)
}

func TestAllocateApplicationSlotUsesFreeInRangeSlotWhenBelowLimit(t *testing.T) {
	slot, err := allocateApplicationSlot([]int{2, 11}, 3)

	require.NoError(t, err)
	require.Equal(t, 1, slot)
}

func TestAllocateApplicationSlotAdvancesAfterConcurrentUniqueConflict(t *testing.T) {
	require.True(t, isRetryableCreateError(&mysql.MySQLError{Number: 1062}))

	slot, err := allocateApplicationSlot([]int{1}, 3)
	require.NoError(t, err)
	require.Equal(t, 2, slot)

	// A concurrent creator can claim slot 2 before this transaction inserts.
	// The retry reloads the slots and must advance instead of weakening quota.
	slot, err = allocateApplicationSlot([]int{1, 2}, 3)
	require.NoError(t, err)
	require.Equal(t, 3, slot)

	_, err = allocateApplicationSlot([]int{1, 2, 3}, 3)
	require.ErrorIs(t, err, appErrors.ErrRateLimited)
}
