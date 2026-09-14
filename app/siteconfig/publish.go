package siteconfig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	appErrors "github.com/mcoder2014/home_server/errors"
	"gorm.io/gorm"
)

var configRequestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (s *Service) Publish(ctx context.Context, namespace string, actorID, expectedRevision int64, request PublishRequest, authorize func(*gorm.DB) error) (*NamespaceView, error) {
	if request.Values == nil {
		return nil, appErrors.ErrInvalid
	}
	return s.publish(ctx, namespace, actorID, expectedRevision, request, nil, authorize)
}

func (s *Service) Rollback(ctx context.Context, namespace string, actorID, expectedRevision int64, request RollbackRequest, authorize func(*gorm.DB) error) (*NamespaceView, error) {
	if request.TargetRevision <= 0 {
		return nil, appErrors.ErrInvalid
	}
	return s.publish(ctx, namespace, actorID, expectedRevision, PublishRequest{RequestID: request.RequestID, Reason: request.Reason}, &request.TargetRevision, authorize)
}

// publish locks runtime -> namespace -> acting account, then records the
// request and value snapshot atomically. Closing registration advances its
// permanent epoch here, outside the generic DAL; rollback never decrements it.
// A successful commit is returned even if refreshing the local snapshot fails.
func (s *Service) publish(ctx context.Context, namespace string, actorID, expectedRevision int64, request PublishRequest, rollbackFrom *int64, authorize func(*gorm.DB) error) (*NamespaceView, error) {
	if s.bootstrap.ConfigSource != "database" {
		return nil, appErrors.ErrForbidden
	}
	if s.database == nil {
		return nil, appErrors.ErrDependency
	}
	if authorize == nil || actorID <= 0 {
		return nil, appErrors.ErrForbidden
	}
	reason := strings.TrimSpace(request.Reason)
	if expectedRevision <= 0 || !configRequestIDPattern.MatchString(request.RequestID) || reason == "" || !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > 512 {
		return nil, appErrors.ErrInvalid
	}
	var candidate map[string]interface{}
	var err error
	if rollbackFrom == nil {
		candidate, err = config.ValidateValues(s.bootstrap, namespace, request.Values)
		if err != nil {
			return nil, appErrors.WithMessage(appErrors.ErrInvalid, err.Error())
		}
	}
	var saved model.SiteConfigCurrent
	// 锁定运行状态和命名空间后重验权限、请求幂等性及修订号，将当前值、历史和代次一起提交。
	err = s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := dal.ReadSiteRuntimeState(tx, true)
		if err != nil {
			return appErrors.ErrDependency
		}
		rows, err := dal.ReadSiteConfigs(tx, []string{namespace}, true)
		if err != nil {
			return appErrors.ErrDependency
		}
		if len(rows) != 1 {
			return appErrors.ErrNotFound
		}
		previous := rows[0]
		oldValues, err := s.checkedValues(previous)
		if err != nil {
			return err
		}
		if err := authorize(tx); err != nil {
			return err
		}
		if rollbackFrom != nil {
			history, err := dal.ReadSiteConfigRevision(tx, namespace, *rollbackFrom)
			if err != nil {
				return appErrors.ErrDependency
			}
			if history == nil {
				return appErrors.ErrNotFound
			}
			candidate, err = s.checkedValues(model.SiteConfigCurrent{Namespace: namespace, Revision: history.Revision, SchemaVersion: history.SchemaVersion, ValuesJSON: history.ValuesJSON, ValuesSHA256: history.ValuesSHA256})
			if err != nil {
				return err
			}
		}
		requestJSON, err := json.Marshal(struct {
			ActorID          int64                  `json:"actor_id"`
			ExpectedRevision int64                  `json:"expected_revision"`
			Reason           string                 `json:"reason"`
			Values           map[string]interface{} `json:"values"`
			RollbackFrom     *int64                 `json:"rollback_from_revision"`
		}{actorID, expectedRevision, reason, candidate, rollbackFrom})
		if err != nil {
			return appErrors.ErrInvalid
		}
		requestHash := sha256.Sum256(requestJSON)
		existing, err := dal.ReadSiteConfigRequest(tx, namespace, request.RequestID)
		if err != nil {
			return appErrors.ErrDependency
		}
		if existing != nil {
			if !bytes.Equal(existing.RequestHash, requestHash[:]) {
				return appErrors.ErrConflict
			}
			saved = model.SiteConfigCurrent{Namespace: namespace, Revision: existing.Revision, SchemaVersion: existing.SchemaVersion, ValuesJSON: existing.ValuesJSON, ValuesSHA256: existing.ValuesSHA256, UpdatedBy: existing.ActorUserID, UpdateTime: existing.CreateTime}
			return nil
		}
		if previous.Revision != expectedRevision {
			return appErrors.ErrConflict
		}
		valuesJSON, err := json.Marshal(candidate)
		if err != nil {
			return appErrors.ErrInvalid
		}
		oldJSON, err := json.Marshal(oldValues)
		if err != nil {
			return appErrors.ErrDependency
		}
		if bytes.Equal(valuesJSON, oldJSON) {
			return appErrors.WithMessage(appErrors.ErrInvalid, "configuration has no changes")
		}
		digest := sha256.Sum256(valuesJSON)
		now := time.Now()
		saved = model.SiteConfigCurrent{Namespace: namespace, Revision: previous.Revision + 1, SchemaVersion: 1, ValuesJSON: string(valuesJSON), ValuesSHA256: digest[:], UpdatedBy: actorID, UpdateTime: now}
		history := model.SiteConfigHistory{Namespace: namespace, Revision: saved.Revision, SchemaVersion: 1, ValuesJSON: saved.ValuesJSON, ValuesSHA256: digest[:], RequestID: request.RequestID, RequestHash: requestHash[:], ActorUserID: actorID, Reason: reason, RollbackFromRevision: rollbackFrom, CreateTime: now}
		if namespace == "registration" && oldValues["enabled"] == true && candidate["enabled"] == false {
			state.RegistrationEpoch++
		}
		state.ConfigGeneration++
		state.Revision++
		state.UpdateTime = now
		if err := dal.SaveSiteConfig(tx, &saved, &history, state); err != nil {
			return appErrors.ErrDependency
		}
		return nil
	})
	if err != nil {
		// Authorization callbacks may carry a domain-specific sentinel; preserve it.
		return nil, err
	}
	_ = s.Refresh(ctx)
	return s.view(saved)
}
