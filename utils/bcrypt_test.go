package utils

import (
	"testing"

	"github.com/sirupsen/logrus"
)

// TestHashAndSalt 确认示例密码可以生成 bcrypt 哈希，并记录生成结果供检查。
func TestHashAndSalt(t *testing.T) {

	tests := []struct {
		name    string
		pwdStr  string
		wantErr bool
	}{
		{
			name:    "test",
			pwdStr:  "passwd",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPwdHash, err := HashAndSalt(tt.pwdStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("HashAndSalt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			logrus.Infof("pwdStr:%v\tresult:%v", tt.pwdStr, gotPwdHash)
		})
	}
}

// TestComparePasswords 用已有 bcrypt 哈希验证正确密码通过、不同密码被拒绝。
func TestComparePasswords(t *testing.T) {
	tests := []struct {
		name      string
		hashedPwd string
		plainPwd  string
		want      bool
	}{
		{
			name:      "passwd",
			hashedPwd: "$2a$04$zUq41XdM3vh2Q6iUGa9hiO/.ZKeTy1BZFXZj1KOACo3qaWGDeufBi",
			plainPwd:  "passwd",
			want:      true,
		},
		{
			name:      "passwd2",
			hashedPwd: "$2a$04$zUq41XdM3vh2Q6iUGa9hiO/.ZKeTy1BZFXZj1KOACo3qaWGDeufBi",
			plainPwd:  "passwd2",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComparePasswords(tt.hashedPwd, tt.plainPwd); got != tt.want {
				t.Errorf("ComparePasswords() = %v, want %v", got, tt.want)
			}
		})
	}
}
