package dal

import (
	"testing"

	"github.com/mcoder2014/home_server/utils"

	"github.com/stretchr/testify/require"

	"github.com/mcoder2014/home_server/domain/model"
)

func TestInsertBookStorage(t *testing.T) {
	s := &model.DBBookStorage{
		BookId:    1,
		Status:    model.StorageStatusNormal,
		Type:      model.StorageTypeOwn,
		Isbn:      "1122334455123",
		Isbn10:    "1234567890",
		LibraryId: 1,
	}

	e := InsertBookStorage(s)
	require.NoError(t, e)

	i, e := QueryBookStorageByIsbn("1122334455123")
	require.NoError(t, e)
	require.Equal(t, s.Status, i.Status)
	require.Equal(t, s.Type, i.Type)
	require.Equal(t, s.BookId, i.BookId)
	require.Equal(t, s.LibraryId, i.LibraryId)
	require.Equal(t, s.Isbn10, i.Isbn10)

	i, e = QueryBookStorageByIsbn10("1234567890")
	require.NoError(t, e)
	require.Equal(t, s.Status, i.Status)
	require.Equal(t, s.Type, i.Type)
	require.Equal(t, s.BookId, i.BookId)
	require.Equal(t, s.LibraryId, i.LibraryId)
	require.Equal(t, s.Isbn10, i.Isbn10)

	updateDto := &model.UpdateBookStorageDto{
		Id:        i.Id,
		BookId:    utils.Int64(2),
		Status:    model.StorageStatusPtr(model.StorageStatusStop),
		Type:      model.StorageTypePtr(model.StorageTypeEbook),
		Isbn:      utils.String("1234567890123"),
		Isbn10:    utils.String("1122334455"),
		LibraryId: utils.Int64(3),
	}
	e = UpdateBookStorage(updateDto)
	i, e = QueryBookStorageByIsbn("1234567890123")
	require.NoError(t, e)
	require.Equal(t, *updateDto.BookId, i.BookId)
	require.Equal(t, *updateDto.Status, i.Status)
	require.Equal(t, *updateDto.Type, i.Type)
	require.Equal(t, *updateDto.Isbn, i.Isbn)
	require.Equal(t, *updateDto.Isbn10, i.Isbn10)
	require.Equal(t, *updateDto.LibraryId, i.LibraryId)

	e = DeleteBookStorageById(i.Id)
	require.NoError(t, e)
}
