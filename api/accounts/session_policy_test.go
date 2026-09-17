package accounts_test

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
)

func TestHTTPWebsiteSessionPolicyAppliesToViewsAndLegacyRollback(t *testing.T) {
	f := newSessionLimitHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	current := requireSuccess(t, f.request("GET", "/api/admin/config/account_policy", admin, nil, nil))
	values := object(current["values"])
	if number(values["max_active_sessions"]) != 5 {
		t.Fatal("a migrated v1 policy did not expose the website session default")
	}
	for _, session := range []*browserSession{admin, member} {
		if number(object(f.me(session)["session_policy"])["max_active_sessions"]) != 5 {
			t.Fatal("account view omitted the applied website session policy")
		}
	}
	schema := requireSuccess(t, f.request("GET", "/api/admin/config/schema", admin, nil, nil))
	found := false
	for _, namespace := range schema["namespaces"].([]interface{}) {
		group := object(namespace)
		if group["namespace"] != "account_policy" {
			continue
		}
		for _, item := range group["fields"].([]interface{}) {
			field := object(item)
			if field["key"] == "max_active_sessions" {
				found = field["type"] == "integer" && number(field["minimum"]) == 1 && number(field["maximum"]) == 100 && number(field["default_value"]) == 5 && field["effect"] == "new_operation"
			}
		}
	}
	if !found {
		t.Fatal("admin configuration schema omitted the bounded website session policy")
	}
	values["max_active_sessions"] = 3
	requireSuccess(t, f.publish(admin, "account_policy", values, "website-session-policy-three"))
	if number(object(f.me(member)["session_policy"])["max_active_sessions"]) != 3 {
		t.Fatal("account view kept an outdated website session policy")
	}
	for _, path := range []string{"/api/account/sessions", "/api/admin/users/" + member.ID + "/sessions"} {
		page := requireSuccess(t, f.request("GET", path, admin, nil, nil))
		if number(page["max_active_sessions"]) != 3 {
			t.Fatal("session page omitted the current website session limit")
		}
	}
	var legacyJSON string
	var legacySHA []byte
	if err := f.database.QueryRow("SELECT values_json,values_sha256 FROM site_config_history WHERE namespace='account_policy' AND revision=1").Scan(&legacyJSON, &legacySHA); err != nil {
		t.Fatal(err)
	}
	var legacy map[string]interface{}
	if err := json.Unmarshal([]byte(legacyJSON), &legacy); err != nil || len(legacy) != 4 || legacy["max_active_sessions"] != nil {
		t.Fatalf("the imported v1 policy must retain its original four fields: %v %v", legacy, err)
	}
	current = requireSuccess(t, f.request("GET", "/api/admin/config/account_policy", admin, nil, nil))
	rolledBack := requireSuccess(t, f.request("POST", "/api/admin/config/account_policy/rollback", admin, map[string]interface{}{"target_revision": 1, "request_id": "website-session-policy-legacy-rollback", "reason": "isolated legacy rollback", "current_password": integrationPassword}, map[string]string{"If-Match": strconv.FormatInt(number(current["revision"]), 10)}))
	if number(object(rolledBack["values"])["max_active_sessions"]) != 5 || number(object(f.me(member)["session_policy"])["max_active_sessions"]) != 5 {
		t.Fatal("legacy rollback did not publish and apply the default website session limit")
	}
	var storedJSON string
	var storedSHA []byte
	if err := f.database.QueryRow("SELECT values_json,values_sha256 FROM site_config_history WHERE namespace='account_policy' AND revision=1").Scan(&storedJSON, &storedSHA); err != nil {
		t.Fatal(err)
	}
	if storedJSON != legacyJSON || !bytes.Equal(storedSHA, legacySHA) {
		t.Fatal("legacy rollback rewrote the imported historical document")
	}
}

func TestHTTPWebsiteSessionPolicyRejectsIncompleteAndInvalidPublications(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.login(f.owner, integrationPassword)
	member := f.login(f.member, integrationPassword)
	current := requireSuccess(t, f.request("GET", "/api/admin/config/account_policy", admin, nil, nil))
	values := object(current["values"])
	values["max_active_sessions"] = 3
	requireSuccess(t, f.publish(admin, "account_policy", values, "website-session-validation-three"))
	current = requireSuccess(t, f.request("GET", "/api/admin/config/account_policy", admin, nil, nil))
	revision := number(current["revision"])
	for _, limit := range []int{1, 100} {
		values["max_active_sessions"] = limit
		requireSuccess(t, f.request("POST", "/api/admin/config/account_policy/validate", admin, map[string]interface{}{"values": values}, nil))
	}
	for index, test := range []struct {
		name    string
		value   interface{}
		missing bool
	}{
		{"missing", nil, true},
		{"null", nil, false},
		{"zero", 0, false},
		{"over maximum", 101, false},
		{"fraction", 1.5, false},
		{"string", "5", false},
		{"boolean", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := make(map[string]interface{}, len(values))
			for key, value := range values {
				candidate[key] = value
			}
			if test.missing {
				delete(candidate, "max_active_sessions")
			} else {
				candidate["max_active_sessions"] = test.value
			}
			requireDenied(t, f.request("POST", "/api/admin/config/account_policy/validate", admin, map[string]interface{}{"values": candidate}, nil))
			requireDenied(t, f.publish(admin, "account_policy", candidate, "website-session-invalid-"+strconv.Itoa(index)))
		})
	}
	values["max_active_sessions"] = 3
	requireDenied(t, f.request("GET", "/api/admin/config/schema", member, nil, nil))
	requireDenied(t, f.request("PUT", "/api/admin/config/account_policy", member, map[string]interface{}{"values": values, "request_id": "website-session-member-denied", "reason": "isolated unauthorized policy update", "current_password": integrationPassword}, map[string]string{"If-Match": strconv.FormatInt(revision, 10)}))
	current = requireSuccess(t, f.request("GET", "/api/admin/config/account_policy", admin, nil, nil))
	if number(current["revision"]) != revision || number(object(current["values"])["max_active_sessions"]) != 3 {
		t.Fatal("invalid or unauthorized publication changed the active policy")
	}
}
