package fileshare

import (
	"github.com/mcoder2014/home_server/config"
	"strings"
	"testing"
	"time"
)

func TestNormalizeShareInputEnforcesModeAndSecretContracts(t *testing.T) {
	tests := []struct {
		name    string
		input   CreateShareInput
		wantErr bool
	}{
		{name: "public without secret", input: CreateShareInput{AccessMode: "public", SecretMode: "none"}},
		{name: "generated public code", input: CreateShareInput{AccessMode: "public", SecretMode: "code"}},
		{name: "explicit public code", input: CreateShareInput{AccessMode: "public", SecretMode: "code", Secret: "aB30Zx"}},
		{name: "authenticated password", input: CreateShareInput{AccessMode: "authenticated", SecretMode: "password", Secret: "eight-byte-secret"}},
		{name: "members", input: CreateShareInput{AccessMode: "members", MemberUserIDs: []string{"42", "7", "42"}, SecretMode: "none"}},
		{name: "public cannot name members", input: CreateShareInput{AccessMode: "public", MemberUserIDs: []string{"42"}, SecretMode: "none"}, wantErr: true},
		{name: "members need members", input: CreateShareInput{AccessMode: "members", SecretMode: "none"}, wantErr: true},
		{name: "code is exactly six ascii alnum", input: CreateShareInput{AccessMode: "public", SecretMode: "code", Secret: "abcdefg"}, wantErr: true},
		{name: "code rejects unicode", input: CreateShareInput{AccessMode: "public", SecretMode: "code", Secret: "密码abcd"}, wantErr: true},
		{name: "password is not a public code", input: CreateShareInput{AccessMode: "public", SecretMode: "password", Secret: "eight-byte-secret"}, wantErr: true},
		{name: "code is not a login password", input: CreateShareInput{AccessMode: "authenticated", SecretMode: "code", Secret: "aB30Zx"}, wantErr: true},
		{name: "password minimum bytes", input: CreateShareInput{AccessMode: "authenticated", SecretMode: "password", Secret: "short"}, wantErr: true},
		{name: "password maximum bytes", input: CreateShareInput{AccessMode: "authenticated", SecretMode: "password", Secret: strings.Repeat("x", 73)}, wantErr: true},
		{name: "negative downloads", input: CreateShareInput{AccessMode: "public", SecretMode: "none", MaxDownloads: -1}, wantErr: true},
		{name: "past expiry", input: CreateShareInput{AccessMode: "public", SecretMode: "none", ExpiresAt: timePointer(time.Unix(99, 0))}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := NormalizeShareInput(test.input, strings.NewReader(strings.Repeat("A", 64)), time.Unix(100, 0))
			if (err != nil) != test.wantErr {
				t.Fatalf("NormalizeShareInput() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && test.input.SecretMode == "code" {
				if len(result.Secret) != 6 || !ValidCode(result.Secret) {
					t.Fatalf("normalized code %q is invalid", result.Secret)
				}
			}
			if err == nil && test.input.AccessMode == "members" && len(result.MemberUserIDs) != 2 {
				t.Fatalf("members were not sorted and deduplicated: %#v", result.MemberUserIDs)
			}
		})
	}
}

func TestPublicStateDoesNotDistinguishTerminalReasons(t *testing.T) {
	now := time.Unix(1000, 0)
	terminal := []ShareSnapshot{
		{FileDeleted: true},
		{RevokedAt: timePointer(now.Add(-time.Second))},
		{ExpiresAt: timePointer(now.Add(-time.Second))},
		{MaxDownloads: 2, DownloadCount: 2},
	}
	for _, snapshot := range terminal {
		state, err := PublicState(snapshot, Viewer{}, false, now)
		if err != ErrNotFound || state != nil {
			t.Fatalf("terminal share leaked its reason: state=%#v err=%v", state, err)
		}
	}
}

func TestPublicStateSeparatesLoginAndSecretWithoutLeakingMembership(t *testing.T) {
	now := time.Unix(1000, 0)
	state, err := PublicState(ShareSnapshot{AccessMode: "authenticated", SecretMode: "password"}, Viewer{}, false, now)
	if err != nil || state.State != "login_required" {
		t.Fatalf("anonymous authenticated state = %#v, %v", state, err)
	}
	state, err = PublicState(ShareSnapshot{AccessMode: "authenticated", SecretMode: "password"}, Viewer{UserID: 7}, false, now)
	if err != nil || state.State != "locked" {
		t.Fatalf("authenticated locked state = %#v, %v", state, err)
	}
	state, err = PublicState(ShareSnapshot{AccessMode: "members", Member: false}, Viewer{UserID: 7}, true, now)
	if err != ErrNotFound || state != nil {
		t.Fatalf("non-member learned share state: %#v, %v", state, err)
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func TestFourByteFileShareSecrets(t *testing.T) {
	original := config.Runtime()
	t.Cleanup(func() {
		if err := config.StoreRuntimeSnapshot(original); err != nil {
			t.Fatal(err)
		}
	})
	policy := original
	policy.AccountPolicy.MinSharePasswordLength = 4
	policy.AccountPolicy.ShareCodeLength = 4
	if err := config.StoreRuntimeSnapshot(policy); err != nil {
		t.Fatal(err)
	}
	for _, input := range []CreateShareInput{
		{AccessMode: "public", SecretMode: "code", Secret: "aB12"},
		{AccessMode: "public", SecretMode: "code"},
		{AccessMode: "authenticated", SecretMode: "password", Secret: "1234"},
	} {
		result, err := NormalizeShareInput(input, strings.NewReader("synthetic-random"), time.Now())
		if err != nil || len(result.Secret) != 4 {
			t.Fatalf("four-byte secret failed: %v %v", result, err)
		}
	}
	if _, err := NormalizeShareInput(CreateShareInput{AccessMode: "authenticated", SecretMode: "password", Secret: "123"}, nil, time.Now()); err == nil {
		t.Fatal("three-byte password accepted")
	}
}
