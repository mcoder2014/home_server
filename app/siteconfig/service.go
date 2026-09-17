package siteconfig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	appErrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

// Service owns the single refresh path. Configuration callbacks cannot modify
// runtime state: authorization is rechecked inside the publication transaction.
type Service struct {
	database      *gorm.DB
	bootstrap     config.Config
	refreshMu     sync.Mutex
	stateMu       sync.RWMutex
	startOnce     sync.Once
	loaded        config.RuntimeConfig
	lastRefreshAt *time.Time
	lastError     string
	bootID        string
	binarySHA256  string
}

func New(database *gorm.DB, bootstrap config.Config) *Service {
	bootstrap.Auth.SiteOrigins = append([]string(nil), bootstrap.Auth.SiteOrigins...)
	bootstrap.Auth.TrustedProxyCIDRs = append([]string(nil), bootstrap.Auth.TrustedProxyCIDRs...)
	service := &Service{database: database, bootstrap: bootstrap, bootID: uuid.NewString()}
	if executable, err := os.Executable(); err == nil {
		if file, err := os.Open(executable); err == nil {
			hash := sha256.New()
			if _, err := io.Copy(hash, file); err == nil {
				service.binarySHA256 = hex.EncodeToString(hash.Sum(nil))
			}
			_ = file.Close()
		}
	}
	return service
}

func (s *Service) Initialize(ctx context.Context) error {
	if s.bootstrap.ConfigSource != "" && s.bootstrap.ConfigSource != "file" && s.bootstrap.ConfigSource != "database" {
		return fmt.Errorf("unsupported config_source")
	}
	return s.Refresh(ctx)
}

func (s *Service) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					refreshCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
					_ = s.Refresh(refreshCtx)
					cancel()
				}
			}
		}()
	})
}

// Refresh 串行重建完整配置快照，数据库模式使用可重复读事务；校验或加载失败时保留上一份有效快照并记录状态。
func (s *Service) Refresh(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	var snapshot config.RuntimeConfig
	var err error
	if s.bootstrap.ConfigSource != "database" {
		snapshot, err = config.BuildRuntimeSnapshot(s.bootstrap, 0, nil, config.DefaultRuntimeValues(s.bootstrap))
	} else {
		if s.database == nil {
			err = appErrors.ErrDependency
		} else {
			// 在同一只读快照中读取代次和全部命名空间，逐项验证后组装待发布的运行时快照。
			err = s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				state, err := dal.ReadSiteRuntimeState(tx, false)
				if err != nil || state.ConfigGeneration < 1 || state.RegistrationEpoch < 1 || state.Revision < 1 {
					return appErrors.ErrDependency
				}
				names := s.namespaceNames()
				rows, err := dal.ReadSiteConfigs(tx, names, false)
				if err != nil || len(rows) != len(names) {
					return appErrors.ErrDependency
				}
				values := make(map[string]map[string]interface{}, len(rows))
				revisions := make(map[string]int64, len(rows))
				for _, row := range rows {
					normalized, err := s.checkedValues(row)
					if err != nil {
						return err
					}
					values[row.Namespace] = normalized
					revisions[row.Namespace] = row.Revision
				}
				snapshot, err = config.BuildRuntimeSnapshot(s.bootstrap, state.ConfigGeneration, revisions, values)
				return err
			}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		}
	}
	if err == nil {
		err = config.StoreRuntimeSnapshot(snapshot)
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if err != nil {
		s.lastError = "configuration refresh failed; last valid snapshot retained"
		return appErrors.ErrDependency
	}
	now := time.Now()
	s.loaded = snapshot
	s.lastRefreshAt = &now
	s.lastError = ""
	return nil
}

func (s *Service) namespaceNames() []string {
	schemas := config.Registry(s.bootstrap)
	names := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		names = append(names, schema.Namespace)
	}
	sort.Strings(names)
	return names
}

func (s *Service) checkedValues(row model.SiteConfigCurrent) (map[string]interface{}, error) {
	if row.Revision < 1 || row.SchemaVersion != 1 {
		return nil, appErrors.ErrDependency
	}
	digest := sha256.Sum256([]byte(row.ValuesJSON))
	if !bytes.Equal(digest[:], row.ValuesSHA256) {
		return nil, appErrors.ErrDependency
	}
	values, err := config.DecodeValues(row.ValuesJSON)
	if err != nil {
		return nil, appErrors.ErrDependency
	}
	// Published v1 account policies omit this additive key; preserve their original JSON and checksum.
	if row.Namespace == "account_policy" && values != nil {
		if _, exists := values["max_active_sessions"]; !exists {
			values["max_active_sessions"] = 5
		}
	}
	normalized, err := config.ValidateValues(s.bootstrap, row.Namespace, values)
	if err != nil {
		return nil, appErrors.ErrDependency
	}
	return normalized, nil
}

// EnabledTx provides a current database policy check. A writer should hold the
// namespace row lock until commit and acquire it before account/resource locks.
func (s *Service) EnabledTx(tx *gorm.DB, namespace, key string, lock bool) (bool, error) {
	var values map[string]interface{}
	var err error
	if s.bootstrap.ConfigSource != "database" {
		values, err = config.ValidateValues(s.bootstrap, namespace, config.DefaultRuntimeValues(s.bootstrap)[namespace])
	} else {
		if tx == nil {
			return false, appErrors.ErrDependency
		}
		rows, readErr := dal.ReadSiteConfigs(tx, []string{namespace}, lock)
		if readErr != nil || len(rows) != 1 {
			return false, appErrors.ErrDependency
		}
		values, err = s.checkedValues(rows[0])
	}
	if err != nil {
		return false, appErrors.ErrDependency
	}
	enabled, ok := values[key].(bool)
	if !ok {
		return false, appErrors.ErrInvalid
	}
	return enabled, nil
}

func (s *Service) Enabled(ctx context.Context, namespace, key string) (bool, error) {
	if s.database == nil {
		return s.EnabledTx(nil, namespace, key, false)
	}
	return s.EnabledTx(s.database.WithContext(ctx), namespace, key, false)
}

// List 返回全部已注册命名空间的配置视图；文件模式使用启动配置对应的生效值，数据库模式要求记录完整。
func (s *Service) List(ctx context.Context) (*NamespaceList, error) {
	result := &NamespaceList{Items: make([]NamespaceView, 0)}
	if s.bootstrap.ConfigSource != "database" {
		defaults := config.DefaultRuntimeValues(s.bootstrap)
		for _, name := range s.namespaceNames() {
			values, err := config.ValidateValues(s.bootstrap, name, defaults[name])
			if err != nil {
				return nil, appErrors.ErrDependency
			}
			result.Items = append(result.Items, NamespaceView{Namespace: name, SchemaVersion: 1, Values: values, ApplyState: "applied"})
		}
		return result, nil
	}
	if s.database == nil {
		return nil, appErrors.ErrDependency
	}
	rows, err := dal.ReadSiteConfigs(s.database.WithContext(ctx), s.namespaceNames(), false)
	if err != nil || len(rows) != len(s.namespaceNames()) {
		return nil, appErrors.ErrDependency
	}
	for _, row := range rows {
		view, err := s.view(row)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, *view)
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, namespace string) (*NamespaceView, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range list.Items {
		if item.Namespace == namespace {
			return &item, nil
		}
	}
	return nil, appErrors.ErrNotFound
}

func (s *Service) view(row model.SiteConfigCurrent) (*NamespaceView, error) {
	values, err := s.checkedValues(row)
	if err != nil {
		return nil, err
	}
	s.stateMu.RLock()
	loadedRevision, lastError := s.loaded.Revisions[row.Namespace], s.lastError
	s.stateMu.RUnlock()
	state := "pending"
	if loadedRevision >= row.Revision {
		state = "applied"
	}
	if lastError != "" {
		state = "stale"
	}
	return &NamespaceView{Namespace: row.Namespace, Revision: row.Revision, SchemaVersion: row.SchemaVersion, Values: values, UpdatedBy: strconv.FormatInt(row.UpdatedBy, 10), UpdateTime: row.UpdateTime, PersistedRevision: row.Revision, LoadedRevision: loadedRevision, ApplyState: state}, nil
}

// Validate 校验完整命名空间候选值，与当前版本逐项比较，返回变化字段及去重后的生效方式。
func (s *Service) Validate(ctx context.Context, namespace string, values map[string]interface{}) (*ValidationResult, error) {
	normalized, err := config.ValidateValues(s.bootstrap, namespace, values)
	if err != nil {
		return nil, appErrors.WithMessage(appErrors.ErrInvalid, err.Error())
	}
	current, err := s.Get(ctx, namespace)
	if err != nil {
		return nil, err
	}
	result := &ValidationResult{Valid: true, Revision: current.Revision, Values: normalized, Changes: []ValueChange{}, Effects: []string{}}
	effects := make(map[string]bool)
	for _, schema := range config.Registry(s.bootstrap) {
		if schema.Namespace != namespace {
			continue
		}
		for _, field := range schema.Fields {
			if field.ReadOnly || reflect.DeepEqual(current.Values[field.Key], normalized[field.Key]) {
				continue
			}
			result.Changes = append(result.Changes, ValueChange{Key: field.Key, Before: current.Values[field.Key], After: normalized[field.Key]})
			effects[field.Effect] = true
		}
	}
	for effect := range effects {
		result.Effects = append(result.Effects, effect)
	}
	sort.Strings(result.Effects)
	return result, nil
}

// History 按修订号游标返回配置历史，校验原始内容摘要但不以当前部署上限阻止历史审计。
func (s *Service) History(ctx context.Context, namespace string, cursor int64, limit int) (*HistoryPage, error) {
	if s.bootstrap.ConfigSource != "database" || s.database == nil {
		return nil, appErrors.ErrForbidden
	}
	known := false
	for _, name := range s.namespaceNames() {
		if name == namespace {
			known = true
		}
	}
	if !known || cursor < 0 {
		return nil, appErrors.ErrInvalid
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := dal.ListSiteConfigHistory(s.database.WithContext(ctx), namespace, cursor, limit+1)
	if err != nil {
		return nil, appErrors.ErrDependency
	}
	result := &HistoryPage{Items: []HistoryView{}, HasMore: len(rows) > limit}
	if result.HasMore {
		rows = rows[:limit]
	}
	for _, row := range rows {
		// History remains auditable when deployment limits are tightened. Only a
		// rollback candidate is validated against the current schema and limits.
		digest := sha256.Sum256([]byte(row.ValuesJSON))
		if !bytes.Equal(digest[:], row.ValuesSHA256) {
			return nil, appErrors.ErrDependency
		}
		values, err := config.DecodeValues(row.ValuesJSON)
		if err != nil {
			return nil, appErrors.ErrDependency
		}
		result.Items = append(result.Items, HistoryView{Namespace: row.Namespace, Revision: row.Revision, Values: values, ActorUserID: strconv.FormatInt(row.ActorUserID, 10), Reason: row.Reason, RequestID: row.RequestID, RollbackFromRevision: row.RollbackFromRevision, CreateTime: row.CreateTime})
	}
	if result.HasMore && len(rows) > 0 {
		result.NextCursor = strconv.FormatInt(rows[len(rows)-1].Revision, 10)
	}
	return result, nil
}

// Status 对照本进程已加载版本与数据库持久化版本，返回待加载或失效状态以及最近刷新信息。
func (s *Service) Status(ctx context.Context) RuntimeStatus {
	s.stateMu.RLock()
	result := RuntimeStatus{BootID: s.bootID, BinarySHA256: s.binarySHA256, LoadedGeneration: s.loaded.Generation, LastError: s.lastError, ApplyState: "applied", Namespaces: []NamespaceStatus{}}
	if s.lastRefreshAt != nil {
		at := *s.lastRefreshAt
		result.LastRefreshAt = &at
	}
	loaded := make(map[string]int64, len(s.loaded.Revisions))
	for ns, revision := range s.loaded.Revisions {
		loaded[ns] = revision
	}
	s.stateMu.RUnlock()
	if s.bootstrap.ConfigSource != "database" {
		return result
	}
	if s.database == nil {
		result.LastError = "configuration database unavailable"
		result.ApplyState = "stale"
		return result
	}
	var rows []model.SiteConfigCurrent
	err := s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := dal.ReadSiteRuntimeState(tx, false)
		if err != nil {
			return err
		}
		result.PersistedGeneration = state.ConfigGeneration
		rows, err = dal.ReadSiteConfigs(tx, s.namespaceNames(), false)
		if err == nil && len(rows) != len(s.namespaceNames()) {
			return appErrors.ErrDependency
		}
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		result.LastError = "configuration database unavailable or incomplete"
	}
	for _, row := range rows {
		state := "applied"
		if _, err := s.checkedValues(row); err != nil {
			result.LastError = "stored configuration is invalid"
		}
		if row.Revision != loaded[row.Namespace] {
			state = "pending"
		}
		if result.LastError != "" {
			state = "stale"
		}
		result.Namespaces = append(result.Namespaces, NamespaceStatus{Namespace: row.Namespace, PersistedRevision: row.Revision, LoadedRevision: loaded[row.Namespace], ApplyState: state})
	}
	if result.PersistedGeneration != result.LoadedGeneration {
		result.ApplyState = "pending"
	}
	if result.LastError != "" || result.LastRefreshAt == nil {
		result.ApplyState = "stale"
	}
	return result
}
