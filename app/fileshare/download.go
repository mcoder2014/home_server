package fileshare

import (
	"context"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/domain/service/accounts"
	service "github.com/mcoder2014/home_server/domain/service/fileshare"
	"github.com/mcoder2014/home_server/domain/service/passport"
	"github.com/mcoder2014/home_server/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Download struct {
	File     *os.File
	Info     os.FileInfo
	Filename string
}

// PrepareDownload opens and validates immutable bytes first, then rechecks the
// module, identities, file, share, ACL, grant and limit under row locks. The
// transaction commits the count before the caller streams; interrupted clients
// deliberately do not receive a refund.
func (application *Application) PrepareDownload(ctx context.Context, token string, principal *utils.Principal, request *http.Request) (*Download, error) {
	access, err := application.resolveShare(ctx, token, principal)
	if err != nil {
		return nil, err
	}
	if err := application.requireAvailable(access, principal, request); err != nil {
		return nil, err
	}
	conf := config.Global().FileSharing
	file, info, err := service.OpenStoredFile(&conf, access.File.StorageKey)
	if err != nil {
		return nil, err
	}
	if info.Size() != access.File.SizeBytes {
		_ = file.Close()
		return nil, service.ErrDependency
	}
	database, err := database(ctx)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	err = database.Transaction(func(tx *gorm.DB) error {
		if err := lockDownloadActors(ctx, tx, access.Share.OwnerUserID, principal); err != nil {
			return err
		}
		currentFile, err := dal.FindFile(tx, access.File.ID, true)
		if err != nil {
			return err
		}
		if currentFile == nil || currentFile.DeletedAt != nil || currentFile.OwnerUserID != access.Share.OwnerUserID || currentFile.StorageKey != access.File.StorageKey || currentFile.SizeBytes != info.Size() || currentFile.SHA256 != access.File.SHA256 {
			return service.ErrNotFound
		}
		currentShare, err := dal.FindShare(tx, access.Share.ID, true)
		if err != nil {
			return err
		}
		if currentShare == nil || currentShare.Token != token || currentShare.FileID != currentFile.ID || currentShare.OwnerUserID != currentFile.OwnerUserID || shareInvariant(currentShare) != nil {
			return service.ErrNotFound
		}
		member := false
		if currentShare.AccessMode == model.FileShareAccessMembers && principal != nil && principal.UserID != currentShare.OwnerUserID {
			member, err = dal.IsFileShareMember(tx, currentShare.ID, principal.UserID, true)
			if err != nil {
				return err
			}
		}
		current := &shareAccess{Share: currentShare, File: currentFile, Member: member}
		if err := application.requireAvailable(current, principal, request); err != nil {
			return err
		}
		updated, err := dal.IncrementDownloadCount(tx, currentShare.ID, application.now())
		if err != nil {
			return err
		}
		if !updated {
			return service.ErrNotFound
		}
		return nil
	})
	if err != nil {
		_ = file.Close()
		return nil, persistenceError(err)
	}
	return &Download{File: file, Info: info, Filename: access.File.OriginalName}, nil
}

func (application *Application) requireAvailable(access *shareAccess, principal *utils.Principal, request *http.Request) error {
	viewerID := int64(0)
	if principal != nil {
		viewerID = principal.UserID
	}
	unlocked := application.grantAuthorized(request, access.Share, application.now())
	state, err := service.PublicState(snapshot(access), service.Viewer{UserID: viewerID}, unlocked, application.now())
	if err != nil {
		return err
	}
	switch state.State {
	case "available":
		return nil
	case "login_required":
		return service.ErrUnauthorized
	default:
		return service.ErrForbidden
	}
}

func lockDownloadActors(ctx context.Context, tx *gorm.DB, ownerID int64, principal *utils.Principal) error {
	if !config.Global().FileSharing.Enabled {
		return service.ErrNotFound
	}
	if !accounts.DatabaseMode() {
		owner, err := passport.GetByID(ctx, ownerID)
		if err != nil {
			return service.ErrDependency
		}
		if owner == nil {
			return service.ErrNotFound
		}
		if principal == nil {
			return nil
		}
		if principal.TokenExpiresAt.IsZero() || !principal.TokenExpiresAt.After(time.Now()) {
			return service.ErrUnauthorized
		}
		if principal.Kind == "user" {
			return passport.RequireConfigSessionTx(ctx, tx, utils.GetTokenFromCtx(ctx), principal.UserID, principal.AuthVersion)
		}
		return accounts.RequireApplicationSnapshotTx(tx, principal, "files:read")
	}
	if principal != nil && principal.Kind == "application" {
		enabled, err := accounts.EnabledTx(tx, "auth", "applications_enabled", true)
		if err != nil || !enabled {
			if err != nil {
				return err
			}
			return service.ErrForbidden
		}
	}
	ids := []int64{ownerID}
	if principal != nil && principal.UserID != ownerID {
		ids = append(ids, principal.UserID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var users []model.UserAccount
	err := tx.Table(dal.AccountTable).Select("id", "status", "must_change_password", "auth_version").Where("id IN ?", ids).Order("id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&users).Error
	if err != nil {
		return service.ErrDependency
	}
	if len(users) != len(ids) {
		return service.ErrNotFound
	}
	for _, user := range users {
		if user.ID == ownerID && (user.Status != model.AccountActive || user.MustChangePassword) {
			return service.ErrNotFound
		}
		if principal != nil && user.ID == principal.UserID && (user.Status != model.AccountActive || user.MustChangePassword || user.AuthVersion != principal.AuthVersion) {
			return service.ErrUnauthorized
		}
	}
	if principal == nil {
		return nil
	}
	if principal.TokenExpiresAt.IsZero() || !principal.TokenExpiresAt.After(time.Now()) {
		return service.ErrUnauthorized
	}
	if principal.Kind == "user" {
		_, err = accounts.RequireUserSessionTx(tx, utils.GetTokenFromCtx(ctx), principal.UserID)
		return err
	}
	return accounts.RequireApplicationSnapshotTx(tx, principal, "files:read")
}
