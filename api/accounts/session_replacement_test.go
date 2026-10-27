package accounts_test

import (
	"crypto/sha256"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestHTTPLoginReplacementRequiresConsentAndRemovesOldestSessions(t *testing.T) {
	for _, limit := range []int{5, 2} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			f := newSessionLimitHTTPFixture(t)
			admin := f.login(f.owner, integrationPassword)
			old := make([]*browserSession, 5)
			for i := range old {
				old[i] = f.login(f.member, integrationPassword)
				digest := sha256.Sum256([]byte(old[i].Cookie.Value))
				stamp := time.Now().UTC().Add(time.Duration(i-5) * time.Minute).Truncate(time.Second)
				if _, err := f.database.Exec("UPDATE login_token SET create_time=?,authenticated_at=? WHERE token_digest=?", stamp, stamp, digest[:]); err != nil {
					t.Fatal(err)
				}
			}
			current := requireSuccess(t, f.request("GET", "/api/admin/config/account_policy", admin, nil, nil))
			values := object(current["values"])
			values["max_active_sessions"] = limit
			if limit != 5 {
				requireSuccess(t, f.publish(admin, "account_policy", values, "lower-session-replacement-limit"))
			}
			input := map[string]interface{}{"user_name": f.member, "password": integrationPassword}
			denied := f.request("POST", "/api/auth/login", nil, input, nil)
			if denied.Status != http.StatusTooManyRequests || denied.Data["can_replace_oldest_session"] != true || len(denied.Cookies) != 0 {
				t.Fatalf("full login must offer replacement without issuing credentials: status=%d data=%v", denied.Status, denied.Data)
			}
			input["replace_oldest_session"] = true
			input["password"] = "incorrect-password"
			wrong := f.request("POST", "/api/auth/login", nil, input, nil)
			requireDenied(t, wrong)
			if wrong.Data["can_replace_oldest_session"] == true {
				t.Fatal("invalid credentials exposed session replacement availability")
			}
			for _, session := range old {
				f.me(session)
			}
			input["password"] = integrationPassword
			replaced := f.request("POST", "/api/auth/login", nil, input, nil)
			identity := requireSuccess(t, replaced)
			var issued *http.Cookie
			for _, cookie := range replaced.Cookies {
				if cookie.MaxAge > 0 {
					if issued != nil {
						t.Fatal("replacement issued multiple live cookies")
					}
					issued = cookie
				}
			}
			if issued == nil || !issued.Secure || !issued.HttpOnly {
				t.Fatal("replacement must establish the usual secure browser session")
			}
			f.me(&browserSession{Cookie: issued, CSRF: identity["csrf_token"].(string)})
			for i, session := range old {
				response := f.request("GET", "/api/auth/me", session, nil, nil)
				if i < len(old)-limit+1 {
					requireDenied(t, response)
				} else {
					requireSuccess(t, response)
				}
			}
			f.me(admin)
			var active int
			if err := f.database.QueryRow("SELECT COUNT(*) FROM login_token WHERE user_id=? AND is_expired=0", old[0].ID).Scan(&active); err != nil || active != limit {
				t.Fatalf("replacement did not converge to the limit: active=%d limit=%d error=%v", active, limit, err)
			}
		})
	}
}

func TestHTTPLoginReplacementRollsBackWhenIssuanceFails(t *testing.T) {
	f := newSessionLimitHTTPFixture(t)
	old := make([]*browserSession, 5)
	for i := range old {
		old[i] = f.login(f.member, integrationPassword)
	}
	if _, err := f.database.Exec("CREATE TRIGGER fail_replacement_insert BEFORE INSERT ON login_token FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='synthetic insert failure'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.database.Exec("DROP TRIGGER IF EXISTS fail_replacement_insert"); err != nil {
			t.Error(err)
		}
	})
	response := f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": f.member, "password": integrationPassword, "replace_oldest_session": true}, nil)
	if response.Status != http.StatusServiceUnavailable || len(response.Cookies) != 0 {
		t.Fatalf("failed issuance must not create a browser session: status=%d", response.Status)
	}
	for _, session := range old {
		f.me(session)
	}
}

func TestHTTPConcurrentLoginReplacementKeepsSessionCap(t *testing.T) {
	f := newSessionLimitHTTPFixture(t)
	for i := 0; i < 5; i++ {
		f.login(f.member, integrationPassword)
	}
	start := make(chan struct{})
	results := make(chan apiResponse, 8)
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			results <- f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": f.member, "password": integrationPassword, "replace_oldest_session": true}, nil)
		}()
	}
	close(start)
	for i := 0; i < cap(results); i++ {
		requireSuccess(t, <-results)
	}
	var active int
	if err := f.database.QueryRow("SELECT COUNT(*) FROM login_token WHERE is_expired=0").Scan(&active); err != nil || active != 5 {
		t.Fatalf("concurrent replacement exceeded the cap: active=%d error=%v", active, err)
	}
}
