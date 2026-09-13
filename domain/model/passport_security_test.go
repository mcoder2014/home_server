package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserIdentityNeverSerializesPasswordHash(t *testing.T) {
	var identity UserIdentity
	if err := json.Unmarshal([]byte(`{"id":42,"user_name":"owner","password":"private-password-hash"}`), &identity); err != nil {
		t.Fatal(err)
	}
	if identity.BcryptPassword != "private-password-hash" {
		t.Fatal("legacy configuration must still decode its password hash")
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-password-hash") || strings.Contains(string(encoded), `"password"`) {
		t.Fatal("serialized user identity exposes a password hash")
	}
}
