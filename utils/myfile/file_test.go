package myfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListDir(t *testing.T) {
	files, dirs, err := ListDir("./")
	require.NoError(t, err)
	t.Logf("files:%v", files)
	t.Logf("dirs:%v", dirs)
}
