package ebook

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileScanner_Scan(t *testing.T) {
	s := &FileScanner{
		ScanPaths:      []string{"/home/chaoqun/storage/电子图书"},
		FilePostfixes:  []string{".pdf", ".epub"},
		ExcludeRegexps: []string{"^\\..*"},
		Fns: []FileScannerCallback{
			func(ctx context.Context, dir, filepath string) error {
				t.Logf("dir:%v, file:%v", dir, filepath)
				return nil
			},
		},
		SkipError: true,
	}
	err := s.Scan(context.Background())
	require.NoError(t, err)
	for _, e := range s.Errors {
		t.Logf("dir:%v file:%v meet error: %v", e.Dir, e.FileName, e.Error)
	}
}

func TestRegexp(t *testing.T) {
	exp := "^\\..*"
	eng, err := regexp.Compile(exp)
	require.NoError(t, err)

	filename := ".test.pdf"
	res := eng.Match([]byte(filename))
	require.Equal(t, true, res)

	filename = "测试文件.pdf"
	res = eng.Match([]byte(filename))
	require.Equal(t, false, res)

}
