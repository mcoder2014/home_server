package accounts

import (
	"context"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/model"
)

type UserDisplay = model.UserDisplay

// DisplayUser exposes only the current public identity, never credentials or contact details.
// Deleted identities hide their previous custom name and image.
func DisplayUser(user *model.UserAccount) UserDisplay {
	if user == nil {
		return UserDisplay{DisplayName: "已注销用户"}
	}
	value := UserDisplay{UserID: user.ID, UserName: user.Username, DisplayName: user.DisplayName, AvatarURL: model.AccountAvatarURL(user)}
	if user.Status == model.AccountDeleted {
		value.DisplayName, value.AvatarURL = "已注销用户", ""
	}
	return value
}

// DisplayUsers batches page-sized current identities without fetching avatar blobs or passwords.
func DisplayUsers(ctx context.Context, ids []int64) (map[int64]UserDisplay, error) {
	result := map[int64]UserDisplay{}
	if len(ids) == 0 {
		return result, nil
	}
	database, err := database(ctx)
	if err != nil {
		return nil, err
	}
	var users []model.UserAccount
	err = database.Table(dal.AccountTable).Select("id", "username", "display_name", "avatar_version", "status", "must_change_password").Where("id IN ?", ids).Find(&users).Error
	if err != nil {
		return nil, normalizeError(err)
	}
	for _, id := range ids {
		result[id] = UserDisplay{UserID: id, DisplayName: "已注销用户"}
	}
	for _, user := range users {
		result[user.ID] = DisplayUser(&user)
	}
	return result, nil
}
