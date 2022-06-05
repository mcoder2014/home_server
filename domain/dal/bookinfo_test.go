package dal

import (
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/mcoder2014/home_server/domain/model"
	"github.com/stretchr/testify/require"

	"github.com/mcoder2014/home_server/utils/testutil"
)

func TestMain(t *testing.M) {
	e := testutil.Init()
	if e != nil {
		logrus.Errorf("e:%v", e)
	}
	t.Run()
}

func TestInsertBookInfo(t *testing.T) {
	e := InsertBookInfo(&model.BookInfo{
		Title:     "测试图书",
		Author:    "江超群",
		Publisher: "小当家出版社",
		Isbn:      "1122334455123",
		Isbn10:    "1234567890",
	})
	require.NoError(t, e)

	b, e := QueryBookInfoByIsbn("1122334455123")
	require.NoError(t, e)
	require.NotNil(t, b)

	b, e = QueryBookInfoByIsbn10("1234567890")
	require.NoError(t, e)
	require.NotNil(t, b)

	books, err := BatchQueryBookInfoByIsbn([]string{"1234567890", "9787115546081"})
	require.NoError(t, err)
	require.True(t, len(books) >= 2)

	e = DeleteBookInfoById(b.Id)
	require.NoError(t, e)
}
