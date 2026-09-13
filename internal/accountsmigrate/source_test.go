package accountsmigrate

import (
	"encoding/json"
	"strings"
	"testing"
)

const fixtureHash = "$2a$04$R2Csdr0qO4JEHxawUyysM.xiv266hVHiNh.hoUCAwpNObRFMyisE6"

func TestSourcePreservesIdentityAndRejectsCrossUserAliases(t *testing.T) {
	raw := []byte(`mysql:
  master_db: "migration:secret@tcp(127.0.0.1:3306)/isolated_test?parseTime=True&loc=Local"
passport:
  mock_data: '[{"id":123,"user_name":"owner","password":"` + fixtureHash + `","email":"OWNER@example.com","mobile":"12345678"}]'
`)
	source, err := ParseSource(raw, Grants{Admins: []int64{123}, Library: []int64{123}, WebDAVWrite: []int64{123}})
	if err != nil {
		t.Fatal(err)
	}
	if source.Users[0].ID != 123 || source.Users[0].PasswordHash != fixtureHash || source.Users[0].Username != "owner" {
		t.Fatal("legacy identity must survive unchanged")
	}
	if source.Users[0].Role != "admin" || source.Users[0].WebDAVPermission != "write" || !source.Users[0].LibraryEnabled {
		t.Fatal("explicit grants lost")
	}
	if len(source.Aliases) != 3 {
		t.Fatal("all legacy login aliases must survive")
	}
	duplicate := strings.Replace(string(raw), `"mobile":"12345678"}]`, `"mobile":"12345678"},{"id":456,"user_name":"OWNER@example.com","password":"`+fixtureHash+`"}]`, 1)
	if _, err = ParseSource([]byte(duplicate), Grants{Admins: []int64{123}}); err == nil {
		t.Fatal("cross-type alias collision must fail")
	}
}

func TestSourceGrantsAreExplicitAndBoundToKnownUsers(t *testing.T) {
	raw := []byte(`passport:
  mock_data: '[{"id":123,"user_name":"owner","password":"` + fixtureHash + `"}]'
`)
	for _, grants := range []Grants{{}, {Admins: []int64{999}}, {Admins: []int64{123}, WebDAVRead: []int64{123}, WebDAVWrite: []int64{123}}} {
		if _, err := ParseSource(raw, grants); err == nil {
			t.Fatalf("invalid grant set accepted: %+v", grants)
		}
	}
	source, err := ParseSource(raw, Grants{Admins: []int64{123}})
	if err != nil {
		t.Fatal(err)
	}
	if source.Users[0].LibraryEnabled || source.Users[0].WebDAVPermission != "none" {
		t.Fatal("admin role must not implicitly grant capabilities")
	}
	data, _ := json.Marshal(source)
	for _, secret := range []string{fixtureHash, "owner", "master_db"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("source JSON exposed private input")
		}
	}
}

func TestSourceRejectsOversizedContactBeforeDDL(t *testing.T) {
	raw := []byte(`passport:
  mock_data: '[{"id":123,"user_name":"owner","password":"` + fixtureHash + `","mobile":"` + strings.Repeat("1", 33) + `"}]'
`)
	if _, err := ParseSource(raw, Grants{Admins: []int64{123}}); err == nil {
		t.Fatal("oversized imported contact accepted")
	}
}
