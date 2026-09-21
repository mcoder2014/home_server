package accounts

import (
	"testing"
	"time"
)

func TestPasswordPolicyDoesNotTruncateOrAcceptMismatchedConfirmation(t *testing.T) {
	for _, tt := range []struct {
		password, confirmation string
		valid                  bool
	}{
		{"a long synthetic password", "a long synthetic password", true},
		{"short", "short", false},
		{"a long synthetic password", "another password", false},
		{string(make([]byte, 73)), string(make([]byte, 73)), false},
	} {
		if got := ValidatePassword(tt.password, tt.confirmation, 15); (got == nil) != tt.valid {
			t.Errorf("password validation valid=%v, expected %v", got == nil, tt.valid)
		}
	}
}

func TestInvitationQuotaUsesSingaporeMonthAndNextMonthEligibility(t *testing.T) {
	now := time.Date(2026, 9, 30, 16, 1, 0, 0, time.UTC)
	month, next := InvitationMonth(now)
	if month != "2026-10" || !next.Equal(time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("incorrect month boundary: %s %s", month, next)
	}
}

func TestWebDAVPermissionChecksEveryReadAndWriteMethod(t *testing.T) {
	for _, method := range []string{"GET", "HEAD", "OPTIONS", "PROPFIND", "PUT", "DELETE", "COPY", "MOVE", "MKCOL", "LOCK", "UNLOCK", "PROPPATCH"} {
		if WebDAVAllowed("none", method) {
			t.Errorf("none allowed %s", method)
		}
		read := method == "GET" || method == "HEAD" || method == "OPTIONS" || method == "PROPFIND"
		if WebDAVAllowed("read", method) != read {
			t.Errorf("read permission incorrect for %s", method)
		}
		if !WebDAVAllowed("write", method) {
			t.Errorf("write denied %s", method)
		}
	}
}

func TestFourCharacterPasswordPolicy(t *testing.T) {
	for _, password := range []string{"1234", "四个字符"} {
		if err := ValidatePassword(password, password, 4); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidatePassword("123", "123", 4); err == nil {
		t.Fatal("three-character password accepted")
	}
	if err := ValidatePassword("1234", "1234", 15); err == nil {
		t.Fatal("default policy was relaxed")
	}
}
