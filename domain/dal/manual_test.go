package dal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManualNamePatternTreatsWildcardsLiterally(t *testing.T) {
	require.Equal(t, `%100!%!_done%`, ManualNameContainsPattern(`100%_done`))
	require.Equal(t, `%a!!b%`, ManualNameContainsPattern(`a!b`))
}
