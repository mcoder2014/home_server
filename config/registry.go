package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

const MaxNamespaceBytes = 64 << 10

type ConfigField struct {
	Key          string      `json:"key"`
	Type         string      `json:"type"`
	Label        string      `json:"label"`
	Unit         string      `json:"unit,omitempty"`
	Minimum      *int64      `json:"minimum,omitempty"`
	Maximum      *int64      `json:"maximum,omitempty"`
	MaxLength    int         `json:"max_length,omitempty"`
	Effect       string      `json:"effect"`
	Public       bool        `json:"public"`
	ReadOnly     bool        `json:"read_only,omitempty"`
	DefaultValue interface{} `json:"default_value"`
}

type NamespaceSchema struct {
	Namespace     string        `json:"namespace"`
	Label         string        `json:"label"`
	SchemaVersion int           `json:"schema_version"`
	Fields        []ConfigField `json:"fields"`
}

// Registry defines the only editable keys. Deployment paths, origins and
// credentials deliberately have no entry in this catalogue.
func Registry(conf Config) []NamespaceSchema {
	hardLimit := conf.UploadHardLimitBytes
	if hardLimit <= 0 {
		hardLimit = conf.WebProjects.MaxUploadBytes
	}
	if hardLimit <= 0 {
		hardLimit = 50 << 20
	}
	groups := []NamespaceSchema{
		{Namespace: "registration", Label: "邀请注册", Fields: []ConfigField{
			{Key: "enabled", Type: "boolean", Label: "允许邀请注册", Effect: "immediate", DefaultValue: false},
			{Key: "monthly_limit", Type: "integer", Label: "每人每月邀请码数量", ReadOnly: true, DefaultValue: 3},
			{Key: "ttl_days", Type: "integer", Label: "邀请码有效天数", ReadOnly: true, DefaultValue: 7},
			{Key: "timezone", Type: "string", Label: "月额度时区", ReadOnly: true, DefaultValue: "Asia/Singapore"},
		}},
		{Namespace: "auth", Label: "应用凭证", Fields: []ConfigField{{Key: "applications_enabled", Type: "boolean", Label: "允许使用应用凭证", Effect: "immediate", DefaultValue: false}}},
		{Namespace: "web_projects", Label: "网页托管", Fields: []ConfigField{
			{Key: "enabled", Type: "boolean", Label: "启用网页托管", Effect: "immediate", DefaultValue: false},
			{Key: "cleanup_enabled", Type: "boolean", Label: "启用到期内容清理", Effect: "immediate", DefaultValue: true},
		}},
		{Namespace: "library", Label: "家庭藏书", Fields: []ConfigField{{Key: "enabled", Type: "boolean", Label: "启用家庭藏书", Effect: "immediate", DefaultValue: true}}},
		{Namespace: "webdav", Label: "WebDAV", Fields: []ConfigField{{Key: "enabled", Type: "boolean", Label: "启用 WebDAV", Effect: "immediate", DefaultValue: true}}},
		{Namespace: "site", Label: "站点展示", Fields: []ConfigField{
			{Key: "title", Type: "string", Label: "站点名称", MaxLength: 80, Effect: "refresh", Public: true, DefaultValue: "CQ Home Server"},
			{Key: "notice", Type: "string", Label: "站点公告", MaxLength: 1000, Effect: "refresh", Public: true, DefaultValue: ""},
		}},
		{Namespace: "account_policy", Label: "账号策略"},
	}
	for _, limit := range []struct {
		namespace, key, label, unit string
		min, max                    int64
		value                       interface{}
	}{
		{"auth", "token_ttl_seconds", "应用 Token 有效期", "秒", 60, 3600, 900},
		{"auth", "default_credential_ttl_days", "应用凭证默认有效期", "天", 1, 3650, 90},
		{"auth", "max_credential_ttl_days", "应用凭证最长有效期", "天", 1, 3650, 365},
		{"auth", "max_applications_per_user", "每人应用数量上限", "个", 1, 100, 20},
		{"web_projects", "max_upload_bytes", "单次上传上限", "bytes", 1, hardLimit, int64(50 << 20)},
		{"web_projects", "max_expanded_bytes", "解压体积上限", "bytes", 1, 1 << 40, int64(200 << 20)},
		{"web_projects", "max_file_bytes", "单文件上限", "bytes", 1, hardLimit, int64(50 << 20)},
		{"web_projects", "max_file_count", "单次文件数量上限", "个", 1, 100000, 5000},
		{"web_projects", "max_directory_depth", "目录深度上限", "层", 1, 64, 16},
		{"web_projects", "max_project_bytes", "单项目体积上限", "bytes", 1, 1 << 40, int64(1 << 30)},
		{"web_projects", "max_projects_per_user", "每人可用项目数量", "个", 1, 100, 10},
		{"web_projects", "max_user_bytes", "每人网页存储上限", "bytes", 1, 1 << 40, int64(10 << 30)},
		{"web_projects", "min_free_disk_bytes", "网页磁盘保留空间", "bytes", 1 << 30, 1 << 40, int64(20 << 30)},
		{"web_projects", "max_releases", "保留版本数量", "个", 1, 1000, 10},
		{"web_projects", "max_concurrent_uploads_per_user", "每人同时上传数量", "个", 1, 100, 2},
		{"web_projects", "max_concurrent_extracts", "全站同时上传数量", "个", 1, 100, 4},
		{"web_projects", "delete_retention_days", "删除后保留时间", "天", 1, 365, 7},
		{"account_policy", "session_ttl_seconds", "用户会话有效期", "秒", 300, 2592000, 604800},
		{"account_policy", "max_active_sessions", "每个账号同时有效的网站登录上限", "个会话", 1, 100, 5},
		{"account_policy", "temporary_password_ttl_days", "临时密码有效期", "天", 1, 30, 7},
		{"account_policy", "min_password_length", "密码最少字符数", "字符", 15, 64, 15},
		{"account_policy", "bcrypt_cost", "密码哈希成本", "", 10, 14, 12},
	} {
		min, max := limit.min, limit.max
		for i := range groups {
			if groups[i].Namespace == limit.namespace {
				groups[i].Fields = append(groups[i].Fields, ConfigField{Key: limit.key, Type: "integer", Label: limit.label, Unit: limit.unit, Minimum: &min, Maximum: &max, Effect: "new_operation", DefaultValue: limit.value})
			}
		}
	}
	for i := range groups {
		groups[i].SchemaVersion = 1
	}
	return groups
}

// ValidateValues accepts a complete namespace document, normalizes numbers to
// int64 and validates related limits together before any database write.
func ValidateValues(conf Config, namespace string, values map[string]interface{}) (map[string]interface{}, error) {
	var definition *NamespaceSchema
	for _, schema := range Registry(conf) {
		if schema.Namespace == namespace {
			copy := schema
			definition = &copy
			break
		}
	}
	if definition == nil {
		return nil, fmt.Errorf("unknown configuration namespace %q", namespace)
	}
	raw, err := json.Marshal(values)
	if err != nil || len(raw) > MaxNamespaceBytes {
		return nil, fmt.Errorf("configuration exceeds JSON size or type limits")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized map[string]interface{}
	if err := decoder.Decode(&normalized); err != nil {
		return nil, err
	}
	expected := 0
	for _, field := range definition.Fields {
		if field.ReadOnly {
			if _, exists := normalized[field.Key]; exists {
				return nil, fmt.Errorf("%s is read only", field.Key)
			}
			continue
		}
		expected++
		value, exists := normalized[field.Key]
		if !exists {
			return nil, fmt.Errorf("missing %s.%s", namespace, field.Key)
		}
		switch field.Type {
		case "boolean":
			if _, ok := value.(bool); !ok {
				return nil, fmt.Errorf("%s must be a boolean", field.Key)
			}
		case "integer":
			number, ok := value.(json.Number)
			if !ok {
				return nil, fmt.Errorf("%s must be an integer", field.Key)
			}
			integer, err := number.Int64()
			if err != nil || integer < *field.Minimum || integer > *field.Maximum {
				return nil, fmt.Errorf("%s is outside the permitted range", field.Key)
			}
			normalized[field.Key] = integer
		case "string":
			text, ok := value.(string)
			if !ok || !utf8.ValidString(text) || utf8.RuneCountInString(text) > field.MaxLength {
				return nil, fmt.Errorf("%s is invalid or too long", field.Key)
			}
			if namespace == "site" && field.Key == "title" && text == "" {
				return nil, fmt.Errorf("site title cannot be empty")
			}
		}
	}
	if len(normalized) != expected {
		return nil, fmt.Errorf("unknown configuration field")
	}
	if namespace == "auth" && normalized["default_credential_ttl_days"].(int64) > normalized["max_credential_ttl_days"].(int64) {
		return nil, fmt.Errorf("default credential TTL exceeds maximum")
	}
	if namespace == "web_projects" {
		keys := []string{"max_file_bytes", "max_upload_bytes", "max_expanded_bytes", "max_project_bytes", "max_user_bytes"}
		for i := 1; i < len(keys); i++ {
			if normalized[keys[i-1]].(int64) > normalized[keys[i]].(int64) {
				return nil, fmt.Errorf("%s cannot exceed %s", keys[i-1], keys[i])
			}
		}
	}
	return normalized, nil
}

// DecodeValues preserves integer precision and rejects trailing JSON documents.
func DecodeValues(raw string) (map[string]interface{}, error) {
	if len(raw) > MaxNamespaceBytes {
		return nil, fmt.Errorf("configuration exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	var values map[string]interface{}
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("unexpected trailing JSON")
	}
	return values, nil
}
