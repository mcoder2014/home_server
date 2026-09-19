package fileshare

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	service "github.com/mcoder2014/home_server/domain/service/fileshare"
	"github.com/mcoder2014/home_server/utils"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func (application *Application) ListShares(ctx context.Context, ownerID, fileID, cursor int64, limit int) (*service.SharePage, error) {
	if ownerID <= 0 || fileID <= 0 || cursor < 0 || limit < 1 || limit > 100 {
		return nil, service.ErrInvalid
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	file, err := dal.FindOwnedFile(database, ownerID, fileID, false, false)
	if err != nil {
		return nil, service.ErrDependency
	}
	if file == nil {
		return nil, service.ErrNotFound
	}
	shares, err := dal.ListOwnedShares(database, ownerID, fileID, cursor, limit+1)
	if err != nil {
		return nil, service.ErrDependency
	}
	page := &service.SharePage{Items: []*service.ShareView{}}
	if len(shares) > limit {
		page.HasMore = true
		shares = shares[:limit]
	}
	ids := make([]int64, 0, len(shares))
	for _, share := range shares {
		ids = append(ids, share.ID)
	}
	members, err := dal.ListShareMembers(database, ids)
	if err != nil {
		return nil, service.ErrDependency
	}
	for _, share := range shares {
		page.Items = append(page.Items, shareView(share, members[share.ID]))
	}
	if page.HasMore {
		page.NextCursor = strconv.FormatInt(shares[len(shares)-1].ID, 10)
	}
	return page, nil
}

func (application *Application) CreateShare(ctx context.Context, principal *utils.Principal, fileID int64, input service.CreateShareInput) (*service.CreateShareResult, error) {
	if principal == nil || principal.UserID <= 0 || fileID <= 0 {
		return nil, service.ErrInvalid
	}
	normalized, err := service.NormalizeShareInput(input, rand.Reader, application.now())
	if err != nil {
		return nil, err
	}
	memberIDs := normalized.MemberUserIDs[:0]
	for _, userID := range normalized.MemberUserIDs {
		if userID != principal.UserID {
			memberIDs = append(memberIDs, userID)
		}
	}
	if normalized.AccessMode == model.FileShareAccessMembers && len(memberIDs) == 0 {
		return nil, service.ErrInvalid
	}
	secretHash := ""
	if normalized.SecretMode != model.FileShareSecretNone {
		cost := config.Runtime().AccountPolicy.BcryptCost
		if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
			cost = bcrypt.DefaultCost
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(normalized.Secret), cost)
		if err != nil {
			return nil, service.ErrDependency
		}
		secretHash = string(hash)
	}
	token, err := service.NewToken(rand.Reader)
	if err != nil {
		return nil, service.ErrDependency
	}
	now := application.now()
	share := &model.FileShare{ID: utils.GenInt64ID(), FileID: fileID, OwnerUserID: principal.UserID, Token: token, AccessMode: normalized.AccessMode, SecretMode: normalized.SecretMode, SecretHash: secretHash, ExpiresAt: normalized.ExpiresAt, MaxDownloads: normalized.MaxDownloads, CreateTime: now, UpdateTime: now}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	err = database.Transaction(func(tx *gorm.DB) error {
		if err := requireWritePolicy(ctx, tx, principal, memberIDs); err != nil {
			return err
		}
		file, err := dal.FindOwnedFile(tx, principal.UserID, fileID, false, true)
		if err != nil {
			return err
		}
		if file == nil {
			return service.ErrNotFound
		}
		var count int64
		if err := tx.Table(dal.FileShareTable).Where("file_id = ?", fileID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 1000 {
			return service.ErrRateLimited
		}
		return dal.InsertFileShare(tx, share, memberIDs)
	})
	if err != nil {
		return nil, persistenceError(err)
	}
	return &service.CreateShareResult{Share: shareView(share, memberIDs), Secret: normalized.Secret}, nil
}

func (application *Application) RevokeShare(ctx context.Context, principal *utils.Principal, fileID, shareID int64) (map[string]interface{}, error) {
	if principal == nil || principal.UserID <= 0 || fileID <= 0 || shareID <= 0 {
		return nil, service.ErrInvalid
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	var revokedAt = application.now()
	err = database.Transaction(func(tx *gorm.DB) error {
		if err := requireWritePolicy(ctx, tx, principal, nil); err != nil {
			return err
		}
		file, err := dal.FindOwnedFile(tx, principal.UserID, fileID, false, true)
		if err != nil {
			return err
		}
		if file == nil {
			return service.ErrNotFound
		}
		share, err := dal.FindOwnedShare(tx, principal.UserID, fileID, shareID, true)
		if err != nil {
			return err
		}
		if share == nil {
			return service.ErrNotFound
		}
		if share.RevokedAt != nil {
			revokedAt = *share.RevokedAt
			return nil
		}
		updated, err := dal.RevokeOwnedShare(tx, principal.UserID, fileID, shareID, revokedAt)
		if err != nil {
			return err
		}
		if !updated {
			return service.ErrConflict
		}
		return nil
	})
	if err != nil {
		return nil, persistenceError(err)
	}
	return map[string]interface{}{"id": strconv.FormatInt(shareID, 10), "revoked_at": revokedAt}, nil
}

func shareView(share *model.FileShare, memberIDs []int64) *service.ShareView {
	members := make([]string, 0, len(memberIDs))
	for _, userID := range memberIDs {
		members = append(members, strconv.FormatInt(userID, 10))
	}
	base := strings.TrimSuffix(config.Global().Auth.SiteOrigin, "/")
	url := "/s/" + share.Token
	if base != "" {
		url = base + url
	}
	return &service.ShareView{ID: strconv.FormatInt(share.ID, 10), FileID: strconv.FormatInt(share.FileID, 10), Token: share.Token, URL: url, AccessMode: share.AccessMode.String(), MemberUserIDs: members, SecretMode: share.SecretMode.String(), ExpiresAt: share.ExpiresAt, MaxDownloads: share.MaxDownloads, DownloadCount: share.DownloadCount, RevokedAt: share.RevokedAt, CreateTime: share.CreateTime}
}

func shareInvariant(share *model.FileShare) error {
	if share == nil || share.ID <= 0 || share.FileID <= 0 || share.OwnerUserID <= 0 || !service.ValidToken(share.Token) || share.AccessMode.String() == "" || share.SecretMode.String() == "" || share.DownloadCount < 0 || share.MaxDownloads < 0 {
		return fmt.Errorf("invalid file share metadata")
	}
	return nil
}
