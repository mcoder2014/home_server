package dal

import (
	"testing"

	"github.com/mcoder2014/home_server/domain/model"
	"github.com/stretchr/testify/require"

	"github.com/mcoder2014/home_server/utils/testutil"
)

func TestMain(t *testing.M) {
	_ = testutil.Init()
	t.Run()
}

func TestInsertBookInfo(t *testing.T) {
	e := InsertBookInfo(&model.BookInfo{
		Title:     "测试图书",
		Author:    "江超群",
		Publisher: "小当家出版社",
		Isbn:      "1123",
		Isbn10:    "1234567890",
	})
	require.NoError(t, e)

	b, e := QueryBookInfoByIsbn("1123")
	require.NoError(t, e)
	require.NotNil(t, b)

	b, e = QueryBookInfoByIsbn10("1234567890")
	require.NoError(t, e)
	require.NotNil(t, b)

	e = DeleteById(b.Id)
	require.NoError(t, e)
}