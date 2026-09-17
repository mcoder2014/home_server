package accounts

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"time"

	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"golang.org/x/image/draw"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const AvatarInputLimit = 2 << 20

var avatarDecoders = make(chan struct{}, 2)

// normalizeAvatar bounds decoder work, rejects unsupported/animated images, then
// crops and reencodes pixels to discard metadata and untrusted input bytes.
func normalizeAvatar(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > AvatarInputLimit {
		return nil, apperrors.ErrInvalid
	}
	select {
	case avatarDecoders <- struct{}{}:
		defer func() { <-avatarDecoders }()
	default:
		return nil, apperrors.ErrRateLimited
	}
	var cfg image.Config
	var err error
	isPNG := bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n"))
	if isPNG {
		// Check structural APNG chunks rather than arbitrary occurrences inside compressed pixels.
		for pos := 8; pos+12 <= len(raw); {
			n := int64(binary.BigEndian.Uint32(raw[pos : pos+4]))
			if n > int64(len(raw)-pos-12) {
				return nil, apperrors.ErrInvalid
			}
			if string(raw[pos+4:pos+8]) == "acTL" {
				return nil, apperrors.ErrInvalid
			}
			pos += int(n) + 12
		}
		cfg, err = png.DecodeConfig(bytes.NewReader(raw))
	} else if bytes.HasPrefix(raw, []byte{0xff, 0xd8}) {
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(raw))
	} else {
		return nil, apperrors.ErrInvalid
	}
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
		return nil, apperrors.ErrInvalid
	}
	var source image.Image
	if isPNG {
		source, err = png.Decode(bytes.NewReader(raw))
	} else {
		source, err = jpeg.Decode(bytes.NewReader(raw))
	}
	if err != nil {
		return nil, apperrors.ErrInvalid
	}
	bounds := source.Bounds()
	size := bounds.Dx()
	if bounds.Dy() < size {
		size = bounds.Dy()
	}
	x, y := bounds.Min.X+(bounds.Dx()-size)/2, bounds.Min.Y+(bounds.Dy()-size)/2
	output := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.Draw(output, output.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(output, output.Bounds(), source, image.Rect(x, y, x+size, y+size), draw.Over, nil)
	var encoded bytes.Buffer
	if err = jpeg.Encode(&encoded, output, &jpeg.Options{Quality: 85}); err != nil || encoded.Len() > 128<<10 {
		return nil, apperrors.ErrInvalid
	}
	return encoded.Bytes(), nil
}

// UpdateAvatar locks the account and acting session before committing the image,
// revision and version pointer together.
// A missing image deletion is idempotent after the supplied revision is checked.
func UpdateAvatar(ctx context.Context, token string, revision int64, raw []byte, remove bool) (*model.UserAccount, error) {
	var normalized []byte
	var err error
	if !remove {
		normalized, err = normalizeAvatar(raw)
		if err != nil {
			return nil, err
		}
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	var id int64
	err = database.Transaction(func(tx *gorm.DB) error {
		user, _, _, e := actingSessionTx(tx, token, true, false)
		if e != nil {
			return e
		}
		id = user.ID
		if user.Revision != revision {
			return apperrors.ErrConflict
		}
		if remove && user.AvatarVersion == 0 {
			return nil
		}
		pointer := revision + 1
		if remove {
			pointer = 0
			if e = tx.Table(dal.AccountAvatarTable).Where("user_id = ?", id).Delete(&model.UserAvatar{}).Error; e != nil {
				return e
			}
		} else {
			avatar := model.UserAvatar{UserID: id, Version: pointer, ContentType: "image/jpeg", ContentBlob: normalized, ByteSize: uint32(len(normalized)), UpdateTime: time.Now()}
			if e = tx.Table(dal.AccountAvatarTable).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"version", "content_type", "content_blob", "byte_size", "update_time"})}).Create(&avatar).Error; e != nil {
				return e
			}
		}
		updated, e := dal.UpdateAccount(tx, id, revision, map[string]interface{}{"avatar_version": pointer, "revision": revision + 1, "update_time": time.Now()})
		if e != nil {
			return e
		}
		if !updated {
			return apperrors.ErrConflict
		}
		return nil
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	return GetByID(ctx, id)
}

// ReadAvatar checks the latest target state and version in the same snapshot as the bytes.
func ReadAvatar(ctx context.Context, id, version int64) ([]byte, error) {
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	var result []byte
	err = database.Transaction(func(tx *gorm.DB) error {
		user, e := dal.QueryAccount(tx, id, false)
		if e != nil {
			return e
		}
		if user == nil || user.Status != model.AccountActive || user.MustChangePassword || version <= 0 || user.AvatarVersion != version {
			return apperrors.ErrNotFound
		}
		var avatar model.UserAvatar
		e = tx.Table(dal.AccountAvatarTable).Select("content_blob", "byte_size", "content_type").Where("user_id = ? AND version = ?", id, version).Take(&avatar).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return apperrors.ErrNotFound
		}
		if e != nil {
			return e
		}
		if avatar.ContentType != "image/jpeg" || len(avatar.ContentBlob) > 128<<10 || len(avatar.ContentBlob) != int(avatar.ByteSize) {
			return apperrors.ErrDependency
		}
		result = avatar.ContentBlob
		return nil
	})
	return result, normalizeError(err)
}
