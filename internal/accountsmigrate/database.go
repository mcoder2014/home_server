package accountsmigrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/go-sql-driver/mysql"
)

type TableSchema struct {
	Name    string   `json:"name"`
	Engine  string   `json:"engine"`
	Columns []Column `json:"columns"`
	Indexes []Index  `json:"indexes"`
	Checks  []Check  `json:"checks"`
}

type queryer interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

// OpenDatabase 根据源配置及可选库名初始化单连接数据库句柄，并关闭多语句执行。
// It never logs a DSN or a server-supplied error containing connection details.
func OpenDatabase(source *Source, databaseOverride string) (*sql.DB, string, error) {
	cfg, err := mysql.ParseDSN(source.Config.Mysql.MasterDB)
	if err != nil {
		return nil, "", errors.New("invalid source database connection configuration")
	}
	if databaseOverride != "" {
		cfg.DBName = databaseOverride
	}
	if !identifier.MatchString(cfg.DBName) || len(cfg.DBName) > 64 {
		return nil, "", errors.New("an explicit valid target database name is required")
	}
	cfg.MultiStatements = false
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, "", errors.New("cannot initialize database connection")
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, cfg.DBName, nil
}

// inspectTable reads column, index and constraint metadata for the reviewed
// migration target. It never changes schema or returns business row values;
// callers compare these definitions before deciding which DDL remains pending.
func inspectTable(ctx context.Context, db queryer, database, table string) (*TableSchema, error) {
	schema := &TableSchema{Name: table}
	err := db.QueryRowContext(ctx, "SELECT ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME=?", database, table).Scan(&schema.Engine)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbError("inspect table", err)
	}
	rows, err := db.QueryContext(ctx, "SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COLUMN_DEFAULT,CHARACTER_SET_NAME,COLLATION_NAME,EXTRA FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=? AND TABLE_NAME=? ORDER BY ORDINAL_POSITION", database, table)
	if err != nil {
		return nil, dbError("inspect columns", err)
	}
	for rows.Next() {
		var column Column
		var nullable, extra string
		var def, charset, collation sql.NullString
		if err = rows.Scan(&column.Name, &column.Type, &nullable, &def, &charset, &collation, &extra); err != nil {
			rows.Close()
			return nil, dbError("read column", err)
		}
		column.Type = normalizeType(column.Type)
		column.Nullable = nullable == "YES"
		if def.Valid {
			column.Default = normalizeDefault(def.String)
		}
		column.Charset = charset.String
		column.Collation = collation.String
		column.AutoIncrement = strings.Contains(strings.ToLower(extra), "auto_increment")
		if match := onUpdatePattern.FindStringSubmatch(extra); match != nil {
			column.OnUpdate = normalizeExpression(match[1])
		}
		schema.Columns = append(schema.Columns, column)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, dbError("read columns", err)
	}
	rows, err = db.QueryContext(ctx, "SELECT INDEX_NAME,COLUMN_NAME,NON_UNIQUE,SUB_PART FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=? AND TABLE_NAME=? ORDER BY INDEX_NAME,SEQ_IN_INDEX", database, table)
	if err != nil {
		return nil, dbError("inspect indexes", err)
	}
	for rows.Next() {
		var name, column string
		var nonunique int
		var subpart sql.NullInt64
		if err = rows.Scan(&name, &column, &nonunique, &subpart); err != nil {
			rows.Close()
			return nil, dbError("read index", err)
		}
		if subpart.Valid {
			column = fmt.Sprintf("%s(%d)", column, subpart.Int64)
		}
		if len(schema.Indexes) == 0 || schema.Indexes[len(schema.Indexes)-1].Name != name {
			schema.Indexes = append(schema.Indexes, Index{Name: name, Unique: nonunique == 0})
		}
		i := len(schema.Indexes) - 1
		schema.Indexes[i].Columns = append(schema.Indexes[i].Columns, column)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, dbError("read indexes", err)
	}
	rows, err = db.QueryContext(ctx, "SELECT CONSTRAINT_NAME,CHECK_CLAUSE FROM information_schema.CHECK_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=? AND TABLE_NAME=? ORDER BY CONSTRAINT_NAME", database, table)
	if err != nil {
		return nil, dbError("inspect checks", err)
	}
	for rows.Next() {
		var check Check
		if err = rows.Scan(&check.Name, &check.Expression); err != nil {
			rows.Close()
			return nil, dbError("read check", err)
		}
		check.Expression = normalizeExpression(check.Expression)
		schema.Checks = append(schema.Checks, check)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, dbError("read checks", err)
	}
	return schema, nil
}

// verifyStep 比较实际表与 DDL 步骤的列、索引和约束；返回是否已完成，定义冲突或已存在的新表结构不完整则报错。
func verifyStep(step Step, schema *TableSchema) (bool, error) {
	if schema == nil {
		if step.Create {
			return false, nil
		}
		return false, fmt.Errorf("required legacy table %s is missing", step.Table)
	}
	if !strings.EqualFold(schema.Engine, "InnoDB") {
		return false, fmt.Errorf("table %s must use InnoDB", step.Table)
	}
	expectedColumns := len(step.Columns)
	// 已发布账号导入定义保持不变，仅接受经过审核的头像指针扩展。
	// 不能放宽成允许所有额外列，否则会掩盖未知结构和错误定义。
	if step.Create && step.ID == "20260913_accounts.sql:001" && step.Table == "user_account" && len(schema.Columns) == expectedColumns+1 {
		for _, column := range schema.Columns {
			if column.Name != "avatar_version" {
				continue
			}
			zero := "0"
			if !reflect.DeepEqual(column, Column{Name: "avatar_version", Type: "bigint", Default: &zero}) {
				return false, errors.New("column definition conflict: user_account.avatar_version")
			}
			expectedColumns++
			break
		}
	}
	if step.Create && (expectedColumns != len(schema.Columns) || len(step.Indexes) != len(schema.Indexes) || len(step.Checks) != len(schema.Checks)) {
		return false, fmt.Errorf("existing table %s has unexpected structure", step.Table)
	}
	complete := true
	for _, expected := range step.Columns {
		var found *Column
		for i := range schema.Columns {
			if schema.Columns[i].Name == expected.Name {
				found = &schema.Columns[i]
				break
			}
		}
		if found == nil {
			complete = false
			continue
		}
		actual := *found
		if expected.Charset == "" {
			if step.Create && actual.Charset != "" && actual.Charset != "utf8mb4" {
				return false, fmt.Errorf("unexpected character set: %s.%s", step.Table, expected.Name)
			}
			actual.Charset = ""
		}
		if expected.Collation == "" {
			actual.Collation = ""
		}
		if !reflect.DeepEqual(expected, actual) {
			return false, fmt.Errorf("column definition conflict: %s.%s", step.Table, expected.Name)
		}
	}
	for _, expected := range step.Indexes {
		found := false
		for _, actual := range schema.Indexes {
			if actual.Name == expected.Name {
				found = true
				if !reflect.DeepEqual(expected, actual) {
					return false, fmt.Errorf("index definition conflict: %s.%s", step.Table, expected.Name)
				}
			}
		}
		if !found {
			complete = false
		}
	}
	for _, expected := range step.Checks {
		found := false
		for _, actual := range schema.Checks {
			if (expected.Name == "" || expected.Name == actual.Name) && expected.Expression == actual.Expression {
				found = true
			}
		}
		if !found {
			complete = false
		}
	}
	if step.Create && !complete {
		return false, fmt.Errorf("existing table %s does not match the complete migration definition", step.Table)
	}
	return complete, nil
}

func dbError(operation string, err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return fmt.Errorf("%s failed (database error %d; details suppressed)", operation, mysqlErr.Number)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s interrupted", operation)
	}
	return fmt.Errorf("%s failed (details suppressed)", operation)
}

func schemaJSON(schemas map[string]*TableSchema) []byte {
	keys := make([]string, 0, len(schemas))
	for key := range schemas {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make([]*TableSchema, 0, len(keys))
	for _, key := range keys {
		ordered = append(ordered, schemas[key])
	}
	raw, _ := json.Marshal(ordered)
	return raw
}

// verifyLegacyTypes checks fields interpreted or modified by this migration;
// unrelated legacy columns and indexes are preserved and fingerprinted.
func verifyLegacyTypes(schema *TableSchema) error {
	required := map[string]map[string]string{
		"bookinfo":                 {"id": "bigint"},
		"book_storage":             {"id": "bigint"},
		"book_address":             {"id": "bigint"},
		"login_token":              {"id": "bigint", "user_id": "bigint", "token": "varchar(256)", "is_expired": "int"},
		"webdav_log":               {"id": "bigint", "user_id": "bigint"},
		"application":              {"id": "bigint", "owner_user_id": "bigint", "revision": "bigint", "secret_version": "bigint"},
		"application_access_token": {"id": "bigint", "application_id": "bigint"},
		"web_project":              {"id": "bigint", "owner_user_id": "bigint", "status": "tinyint unsigned", "access_mode": "tinyint unsigned", "current_release_id": "bigint", "revision": "bigint"},
		"web_project_member":       {"project_id": "bigint", "user_id": "bigint", "created_by": "bigint"},
		"web_project_release":      {"id": "bigint", "project_id": "bigint", "uploaded_by": "bigint", "storage_key": "varchar(512)", "status": "tinyint unsigned", "file_count": "int", "total_bytes": "bigint"},
	}
	for name, expected := range required[schema.Name] {
		found := false
		for _, column := range schema.Columns {
			if column.Name == name && column.Type == expected {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("legacy column is missing or incompatible: %s.%s", schema.Name, name)
		}
	}
	return nil
}
