package accounts_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"testing"

	passportapi "github.com/mcoder2014/home_server/api/passport"
	passportservice "github.com/mcoder2014/home_server/domain/service/passport"
)

// TestHTTPAnonymousFailuresCannotBlockAuthenticatedPasswordConfirmation 验证匿名账号和来源预算耗尽后，已验证管理员会话仍可执行密码确认动作。
func TestHTTPAnonymousFailuresCannotBlockAuthenticatedPasswordConfirmation(t *testing.T) {
	f := newHTTPFixture(t, false)
	for _, statement := range []string{"UPDATE user_account SET id=8803001 WHERE id=1001", "UPDATE user_login_alias SET user_id=8803001 WHERE user_id=1001"} {
		if _, err := f.database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	f.ip = "198.51.100.210:45000"
	owner := f.login(f.owner, integrationPassword)
	requireSuccess(t, f.publish(owner, "registration", map[string]interface{}{"enabled": true}, "authenticated-budget-open"))
	for i := 0; i < 30; i++ {
		name := f.owner
		if i >= 10 {
			name = fmt.Sprintf("missing-budget-%d", i)
		}
		requireDenied(t, f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": name, "password": "anonymous wrong password"}, nil))
	}
	spoofed := map[string]string{"X-Password-Budget-Scope": "authenticated", "home_server.password_budget_scope": "authenticated"}
	login := f.request("POST", "/api/auth/login", owner, map[string]interface{}{"user_name": f.owner, "password": integrationPassword}, spoofed)
	if login.Status != 429 {
		t.Fatalf("anonymous login budget was bypassed by cookies or forged scope headers: HTTP %d", login.Status)
	}
	requireSuccess(t, f.request("GET", "/api/auth/me", owner, nil, nil))
	requireSuccess(t, f.adminAction(owner, "1002", "ban", "POST", map[string]interface{}{}))
	requireSuccess(t, f.publish(owner, "registration", map[string]interface{}{"enabled": false}, "authenticated-budget-close"))
}

// TestHTTPAuthenticatedPasswordFailuresKeepTheirOwnLimit 验证已登录的错误密码确认仍受账号限流，且不会耗尽匿名登录预算。
func TestHTTPAuthenticatedPasswordFailuresKeepTheirOwnLimit(t *testing.T) {
	f := newHTTPFixture(t, false)
	for _, statement := range []string{"UPDATE user_account SET id=8803002 WHERE id=1001", "UPDATE user_login_alias SET user_id=8803002 WHERE user_id=1001"} {
		if _, err := f.database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	f.ip = "198.51.100.211:45001"
	owner := f.login(f.owner, integrationPassword)
	for i := 0; i < 10; i++ {
		requireDenied(t, f.request("POST", "/api/auth/change-password", owner, map[string]interface{}{"current_password": "wrong confirmed password", "new_password": integrationNextPassword, "confirm_password": integrationNextPassword}, nil))
	}
	limited := f.publish(owner, "registration", map[string]interface{}{"enabled": false}, "confirmed-budget-limited")
	if limited.Status != 429 {
		t.Fatalf("authenticated password confirmations did not share their failure limit: HTTP %d", limited.Status)
	}
	login := f.request("POST", "/api/auth/login", nil, map[string]interface{}{"user_name": f.owner, "password": integrationPassword}, nil)
	requireSuccess(t, login)
}

// TestHTTPLegacyRSALoginKeepsValidProtocol 使用真实 RSA 公钥和数据库账号完成旧协议登录，验证所得令牌可通过账号会话认证。
func TestHTTPLegacyRSALoginKeepsValidProtocol(t *testing.T) {
	f := newHTTPFixture(t, false)
	f.ip = "198.51.100.212:45002"
	f.router.POST("/passport/login", passportapi.Login)
	public, _, err := passportservice.GetLoginRsa(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(public)
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := rsa.EncryptPKCS1v15(rand.Reader, key.(*rsa.PublicKey), []byte(integrationPassword))
	if err != nil {
		t.Fatal(err)
	}
	result := requireSuccess(t, f.request("POST", "/passport/login", nil, map[string]string{"user_name": f.owner, "crypt_passwd": base64.StdEncoding.EncodeToString(cipher)}, nil))
	token, ok := result["token"].(string)
	if !ok || token == "" {
		t.Fatal("legacy RSA login did not return its historical token field")
	}
	requireSuccess(t, f.request("GET", "/api/auth/me", nil, nil, map[string]string{"passport": token}))
}
