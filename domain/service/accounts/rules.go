package accounts

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
var invitationLocation = time.FixedZone("Asia/Singapore", 8*60*60)

func ValidatePassword(password, confirmation string, minimum int) error {
	if minimum < 4 {
		minimum = 15
	}
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < minimum || len(password) > 72 || strings.ContainsRune(password, '\x00') {
		return apperrors.WithMessage(apperrors.ErrInvalid, fmt.Sprintf("密码至少%d个字符，最多72个UTF-8字节", minimum))
	}
	if password != confirmation {
		return apperrors.WithMessage(apperrors.ErrInvalid, "两次输入的密码不一致")
	}
	return nil
}

func InvitationMonth(now time.Time) (string, time.Time) {
	local := now.In(invitationLocation)
	next := time.Date(local.Year(), local.Month()+1, 1, 0, 0, 0, 0, invitationLocation)
	return local.Format("2006-01"), next
}

func WebDAVAllowed(permission, method string) bool {
	if permission == model.WebDAVWrite {
		return true
	}
	if permission != model.WebDAVRead {
		return false
	}
	switch method {
	case "GET", "HEAD", "OPTIONS", "PROPFIND":
		return true
	default:
		return false
	}
}

func NormalizeUsername(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !usernamePattern.MatchString(value) {
		return "", apperrors.WithMessage(apperrors.ErrInvalid, "用户名需为3至32位字母、数字、下划线或短横线")
	}
	return value, nil
}

func validateProfile(name, email, mobile string) error {
	if !utf8.ValidString(name) || utf8.RuneCountInString(strings.TrimSpace(name)) > 64 || len(email) > 254 || len(mobile) > 32 {
		return apperrors.ErrInvalid
	}
	if email != "" {
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email {
			return apperrors.WithMessage(apperrors.ErrInvalid, "联系邮箱格式错误")
		}
	}
	for _, r := range mobile {
		if r != '+' && r != '-' && r != ' ' && (r < '0' || r > '9') {
			return apperrors.WithMessage(apperrors.ErrInvalid, "联系电话格式错误")
		}
	}
	return nil
}

func randomSecret(prefix string, size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", apperrors.ErrDependency
	}
	return prefix + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func randomPassword(minimum int) (string, error) {
	if minimum < 20 {
		minimum = 20
	}
	if minimum > 72 {
		return "", apperrors.ErrInvalid
	}
	// Base64URL is ASCII; rounding up the entropy byte count meets the minimum
	// without exceeding bcrypt's 72-byte limit, including the maximum policy.
	return randomSecret("", (minimum*3+3)/4)
}
