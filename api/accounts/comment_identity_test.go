package accounts_test

import (
	"strconv"
	"testing"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/passport"
)

func publicCommentIdentityPage(t *testing.T, f *httpFixture, owner *browserSession) (string, string) {
	t.Helper()
	project, release := f.privatePage(owner)
	base := "/api/web-share/" + project["id"].(string)
	requireSuccess(t, f.request("PATCH", base, owner, map[string]interface{}{"access_mode": "public"}, map[string]string{"If-Match": strconv.FormatInt(number(project["revision"]), 10)}))
	return base + "/comment-threads", release
}

func requireCommentIdentity(t *testing.T, value map[string]interface{}, prefix, id, name, snapshot string) {
	t.Helper()
	identity := object(value[prefix+"_user"])
	current := identity["display_name"]
	if current == "" {
		current = identity["user_name"]
	}
	if identity["user_id"] != id || current != name || value[prefix+"_name_snapshot"] != snapshot {
		t.Fatalf("comment %s must expose current identity %s/%s and display snapshot %s: %v", prefix, id, name, snapshot, value)
	}
	if name == "已注销用户" && identity["avatar_url"] != "" {
		t.Fatal("deleted comment identity exposed an avatar")
	}
}

func requireStoredCommentIdentity(t *testing.T, f *httpFixture, threadID, name string) {
	t.Helper()
	var threadName, eventName string
	if err := f.database.QueryRow("SELECT author_name_snapshot FROM web_comment_thread WHERE id=?", threadID).Scan(&threadName); err != nil {
		t.Fatal(err)
	}
	if err := f.database.QueryRow("SELECT actor_name_snapshot FROM web_comment_event WHERE thread_id=? AND sequence=1", threadID).Scan(&eventName); err != nil {
		t.Fatal(err)
	}
	if threadName != name || eventName != name {
		t.Fatalf("current identity projection rewrote historical evidence: thread=%q event=%q", threadName, eventName)
	}
}

func TestHTTPCommentIdentityResetUsesCurrentNamesInEveryResponse(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	author := f.login(f.member, integrationPassword)
	reader := f.login(f.guest, integrationPassword)
	base, release := publicCommentIdentityPage(t, f, owner)
	oldName := "需重置的历史昵称"
	requireSuccess(t, f.request("PATCH", "/api/account/profile", author, map[string]string{"display_name": oldName}, map[string]string{"If-Match": strconv.FormatInt(author.Revision, 10)}))
	input := map[string]interface{}{"request_id": "identity-before-reset", "release_id": release, "page_key": "path:index.html", "page_path": "index.html", "anchor": map[string]interface{}{"kind": "page"}, "body": "Synthetic identity comment"}
	created := requireSuccess(t, f.request("POST", base, author, input, nil))
	id := created["id"].(string)
	path := base + "/" + id
	requireSuccess(t, f.adminAction(owner, author.ID, "reset-profile", "POST", map[string]interface{}{"reset_display_name": true}))
	for _, endpoint := range []string{base, path} {
		t.Run(endpoint, func(t *testing.T) {
			value := requireSuccess(t, f.request("GET", endpoint, reader, nil, nil))
			if endpoint == base {
				value = object(value["items"].([]interface{})[0])
			}
			requireCommentIdentity(t, value, "author", author.ID, f.member, f.member)
		})
	}
	t.Run("events", func(t *testing.T) {
		value := requireSuccess(t, f.request("GET", path+"/events", reader, nil, nil))
		requireCommentIdentity(t, object(value["items"].([]interface{})[0]), "actor", author.ID, f.member, f.member)
	})
	t.Run("idempotent comment", func(t *testing.T) {
		value := requireSuccess(t, f.request("POST", base, author, input, nil))
		requireCommentIdentity(t, value, "author", author.ID, f.member, f.member)
		if value["id"] != id {
			t.Fatal("identity refresh duplicated the comment")
		}
	})
	for i := 0; i < 2; i++ {
		t.Run("reply "+strconv.Itoa(i), func(t *testing.T) {
			value := requireSuccess(t, f.request("POST", path+"/replies", reader, map[string]interface{}{"request_id": "identity-after-reset", "body": "Synthetic reply"}, nil))
			requireCommentIdentity(t, value, "author", author.ID, f.member, f.member)
		})
	}
	requireStoredCommentIdentity(t, f, id, oldName)
}

func TestHTTPCommentIdentityDeletedAuthorHidesNamesButKeepsEvidence(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	author := f.login(f.member, integrationPassword)
	reader := f.login(f.guest, integrationPassword)
	base, release := publicCommentIdentityPage(t, f, owner)
	oldName := "已删除账号的历史昵称"
	requireSuccess(t, f.request("PATCH", "/api/account/profile", author, map[string]string{"display_name": oldName}, map[string]string{"If-Match": strconv.FormatInt(author.Revision, 10)}))
	created := requireSuccess(t, f.request("POST", base, author, map[string]interface{}{"request_id": "identity-before-delete", "release_id": release, "page_key": "path:index.html", "page_path": "index.html", "anchor": map[string]interface{}{"kind": "page"}, "body": "Synthetic identity comment"}, nil))
	id := created["id"].(string)
	path := base + "/" + id
	reply := map[string]interface{}{"request_id": "identity-reply-before-delete", "body": "Synthetic reply"}
	requireSuccess(t, f.request("POST", path+"/replies", reader, reply, nil))
	requireSuccess(t, f.adminAction(owner, author.ID, "delete", "POST", map[string]interface{}{}))
	for _, endpoint := range []string{base, path} {
		t.Run(endpoint, func(t *testing.T) {
			value := requireSuccess(t, f.request("GET", endpoint, reader, nil, nil))
			if endpoint == base {
				value = object(value["items"].([]interface{})[0])
			}
			requireCommentIdentity(t, value, "author", author.ID, "已注销用户", "已注销用户")
		})
	}
	t.Run("events", func(t *testing.T) {
		value := requireSuccess(t, f.request("GET", path+"/events", reader, nil, nil))
		requireCommentIdentity(t, object(value["items"].([]interface{})[0]), "actor", author.ID, "已注销用户", "已注销用户")
	})
	t.Run("idempotent reply", func(t *testing.T) {
		value := requireSuccess(t, f.request("POST", path+"/replies", reader, reply, nil))
		requireCommentIdentity(t, value, "author", author.ID, "已注销用户", "已注销用户")
	})
	t.Run("new reply", func(t *testing.T) {
		value := requireSuccess(t, f.request("POST", path+"/replies", reader, map[string]interface{}{"request_id": "identity-reply-after-delete", "body": "New synthetic reply"}, nil))
		requireCommentIdentity(t, value, "author", author.ID, "已注销用户", "已注销用户")
	})
	requireStoredCommentIdentity(t, f, id, oldName)
}

func TestHTTPCommentIdentityApplicationsKeepTheirOwnNamesAfterReset(t *testing.T) {
	f := newHTTPFixture(t)
	owner := f.login(f.owner, integrationPassword)
	author := f.login(f.member, integrationPassword)
	reader := f.login(f.guest, integrationPassword)
	base, release := publicCommentIdentityPage(t, f, owner)
	token := f.applicationToken(author, []string{"web-comments:write"})
	headers := map[string]string{"Authorization": "Bearer " + token}
	input := map[string]interface{}{"request_id": "application-identity-comment", "release_id": release, "page_key": "path:index.html", "page_path": "index.html", "anchor": map[string]interface{}{"kind": "page"}, "body": "Synthetic application comment"}
	created := requireSuccess(t, f.request("POST", base, nil, input, headers))
	id := created["id"].(string)
	appName := "HTTP fixture application"
	requireSuccess(t, f.adminAction(owner, author.ID, "reset-profile", "POST", map[string]interface{}{"reset_display_name": true}))
	for _, endpoint := range []string{base, base + "/" + id} {
		value := requireSuccess(t, f.request("GET", endpoint, reader, nil, nil))
		if endpoint == base {
			value = object(value["items"].([]interface{})[0])
		}
		requireCommentIdentity(t, value, "author", author.ID, f.member, appName)
		if value["author_application_id"] == "0" {
			t.Fatal("application author lost its application marker")
		}
	}
	events := requireSuccess(t, f.request("GET", base+"/"+id+"/events", reader, nil, nil))
	requireCommentIdentity(t, object(events["items"].([]interface{})[0]), "actor", author.ID, f.member, appName)
	idempotent := requireSuccess(t, f.request("POST", base, nil, input, headers))
	requireCommentIdentity(t, idempotent, "author", author.ID, f.member, appName)
	requireStoredCommentIdentity(t, f, id, appName)
}

func TestHTTPCommentIdentityLegacyConfigurationUsesRealUserName(t *testing.T) {
	f := newHTTPFixture(t)
	databaseOwner := f.login(f.owner, integrationPassword)
	base, release := publicCommentIdentityPage(t, f, databaseOwner)
	before := config.Global()
	legacy := before
	legacy.IdentitySource = "file"
	config.SetGlobalConfig(legacy)
	if err := passport.Init(&legacy); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { config.SetGlobalConfig(before) })
	author := f.login(f.owner, integrationPassword)
	reader := f.login(f.guest, integrationPassword)
	input := map[string]interface{}{"request_id": "legacy-identity-comment", "release_id": release, "page_key": "path:index.html", "page_path": "index.html", "anchor": map[string]interface{}{"kind": "page"}, "body": "Synthetic legacy comment"}
	created := requireSuccess(t, f.request("POST", base, author, input, nil))
	id := created["id"].(string)
	t.Run("create", func(t *testing.T) {
		requireCommentIdentity(t, created, "author", author.ID, f.owner, f.owner)
	})
	for _, endpoint := range []string{base, base + "/" + id} {
		t.Run(endpoint, func(t *testing.T) {
			value := requireSuccess(t, f.request("GET", endpoint, reader, nil, nil))
			if endpoint == base {
				value = object(value["items"].([]interface{})[0])
			}
			requireCommentIdentity(t, value, "author", author.ID, f.owner, f.owner)
			if object(value["author_user"])["avatar_url"] != "" {
				t.Fatal("config identity unexpectedly exposed a database avatar")
			}
		})
	}
	t.Run("events", func(t *testing.T) {
		value := requireSuccess(t, f.request("GET", base+"/"+id+"/events", reader, nil, nil))
		requireCommentIdentity(t, object(value["items"].([]interface{})[0]), "actor", author.ID, f.owner, f.owner)
	})
	t.Run("idempotent comment", func(t *testing.T) {
		value := requireSuccess(t, f.request("POST", base, author, input, nil))
		requireCommentIdentity(t, value, "author", author.ID, f.owner, f.owner)
	})
	t.Run("new reply", func(t *testing.T) {
		value := requireSuccess(t, f.request("POST", base+"/"+id+"/replies", reader, map[string]interface{}{"request_id": "legacy-identity-reply", "body": "Synthetic legacy reply"}, nil))
		requireCommentIdentity(t, value, "author", author.ID, f.owner, f.owner)
	})
	requireStoredCommentIdentity(t, f, id, "用户 "+author.ID)
}
