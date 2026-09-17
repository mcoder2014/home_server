package accounts

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mcoder2014/home_server/domain/model"
)

func TestDeletedDisplayPreservesStableUsernameWithoutCustomIdentity(t *testing.T) {
	identity := DisplayUser(&model.UserAccount{ID: 1002, Username: "stable_login", DisplayName: "previous_custom_name", AvatarVersion: 3, Status: model.AccountDeleted, PasswordHash: "private"})
	if identity.UserName != "stable_login" || identity.DisplayName != "已注销用户" || identity.AvatarURL != "" {
		t.Fatal("deleted display broke legacy username compatibility or exposed custom identity")
	}
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "previous_custom_name") || strings.Contains(string(raw), "private") {
		t.Fatal("minimal display contains historical custom identity or credentials")
	}
}
