package dal

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func manualTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("MANUALS_TEST_DSN")
	if dsn == "" {
		t.Skip("MANUALS_TEST_DSN is required for MariaDB manual tests")
	}
	parsed, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(parsed.DBName, "home_server_manuals_test_"), "refuse non-fixture database")
	database, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	connection, err := database.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = connection.Close()
	})
	var actual string
	require.NoError(t, database.Raw("SELECT DATABASE()").Scan(&actual).Error)
	require.Equal(t, parsed.DBName, actual)
	for _, table := range []string{ManualCategoryTable, ManualItemTable, ManualTable} {
		require.NoError(t, database.Exec("DROP TABLE IF EXISTS "+table).Error)
	}
	ddl, err := os.ReadFile("migrations/20260919_manuals.sql")
	require.NoError(t, err)
	for _, statement := range strings.Split(string(ddl), ";") {
		if strings.TrimSpace(statement) != "" {
			require.NoError(t, database.Exec(statement).Error)
		}
	}
	return database
}

func TestManualMigrationAndLiteralSearchCategories(t *testing.T) {
	database := manualTestDatabase(t)
	now := time.Now()
	manuals := []*model.Manual{
		{ID: 101, OwnerUserID: 1, Name: "维修100%指南", AccessMode: model.ManualAccessPublic, Status: model.ManualStatusActive, Revision: 1, ClientRequestID: "CaseKey", RequestHash: strings.Repeat("a", 64), CreateTime: now, UpdateTime: now},
		{ID: 102, OwnerUserID: 1, Name: "维修100x指南", AccessMode: model.ManualAccessPublic, Status: model.ManualStatusActive, Revision: 1, ClientRequestID: "request-102", RequestHash: strings.Repeat("b", 64), CreateTime: now, UpdateTime: now},
		{ID: 103, OwnerUserID: 1, Name: "A_B手册", AccessMode: model.ManualAccessPublic, Status: model.ManualStatusActive, Revision: 1, ClientRequestID: "request-103", RequestHash: strings.Repeat("c", 64), CreateTime: now, UpdateTime: now},
		{ID: 104, OwnerUserID: 1, Name: "AxB手册", AccessMode: model.ManualAccessPublic, Status: model.ManualStatusActive, Revision: 1, ClientRequestID: "request-104", RequestHash: strings.Repeat("d", 64), CreateTime: now, UpdateTime: now},
		{ID: 105, OwnerUserID: 1, Name: "A!B手册", AccessMode: model.ManualAccessPublic, Status: model.ManualStatusActive, Revision: 1, ClientRequestID: "request-105", RequestHash: strings.Repeat("e", 64), CreateTime: now, UpdateTime: now},
		{ID: 106, OwnerUserID: 1, Name: "冰箱ABC手册", AccessMode: model.ManualAccessPublic, Status: model.ManualStatusActive, Revision: 1, ClientRequestID: "request-106", RequestHash: strings.Repeat("f", 64), CreateTime: now, UpdateTime: now},
		{ID: 107, OwnerUserID: 1, Name: "大小写请求", AccessMode: model.ManualAccessPublic, Status: model.ManualStatusActive, Revision: 1, ClientRequestID: "casekey", RequestHash: strings.Repeat("0", 64), CreateTime: now, UpdateTime: now},
	}
	for _, manual := range manuals {
		require.NoError(t, InsertManual(database, manual))
	}
	require.NoError(t, InsertManualCategories(database, 101, []string{"Kitchen", "厨房"}))
	require.NoError(t, InsertManualCategories(database, 102, []string{"kitchen"}))
	require.NoError(t, InsertManualCategories(database, 103, []string{"厨房"}))

	for query, expected := range map[string][]int64{
		"%":     {101},
		"_":     {103},
		"!":     {105},
		"冰箱abc": {106},
	} {
		rows, err := ListManuals(database, ManualListFilter{Query: query, Limit: 100})
		require.NoError(t, err)
		require.Equal(t, expected, manualIDs(rows), query)
	}
	rows, err := ListManuals(database, ManualListFilter{CategorySet: true, Category: "", Limit: 100})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{104, 105, 106, 107}, manualIDs(rows))
	rows, err = ListManuals(database, ManualListFilter{CategorySet: true, Category: "Kitchen", Limit: 100})
	require.NoError(t, err)
	require.Equal(t, []int64{101}, manualIDs(rows))
	rows, err = ListManuals(database, ManualListFilter{CategorySet: true, Category: "kitchen", Limit: 100})
	require.NoError(t, err)
	require.Equal(t, []int64{102}, manualIDs(rows))

	categories, err := ListManualCategories(database, ManualListFilter{}, 100)
	require.NoError(t, err)
	require.Equal(t, []string{"Kitchen", "kitchen", "厨房"}, categories)
	categoryRows, err := ListManualCategoriesByManualIDs(database, []int64{101, 104})
	require.NoError(t, err)
	require.Equal(t, []*model.ManualCategory{{ManualID: 101, Category: "Kitchen"}, {ManualID: 101, Category: "厨房"}}, categoryRows)
}

func manualIDs(rows []*model.Manual) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}
