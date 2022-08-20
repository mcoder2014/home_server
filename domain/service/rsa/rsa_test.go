package rsa

import (
	"encoding/base64"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecrypt(t *testing.T) {
	tests := []struct {
		name       string
		privateKey string
		cryData    string
		want       string
		wantErr    bool
	}{
		{
			name:       "normal",
			privateKey: "-----BEGIN RSA PRIVATE KEY-----\nMIICXgIBAAKBgQC6WpyFg7jruJxt30LImfXWUCc75hT+DbSeDElu7bVOKShYdlrx\nPkT3jAIdM1tJRabDxiIV/kWyKdy3MYZ5gOobpqjYkaeYgiVl9MZanPoX0qpEMTeF\naELpPC691UpMsq76Ejq+3tnnsBSPYlmRNgV5emVIP2zuOZOLdZuRqe+e7wIDAQAB\nAoGBAKAOrXsbjNuhP3I7HSAw5G6Df385+fPPD7/jq7rEHkIYpZd9aFTl99Rqg3JT\nJufDFB34clRThccln3YU6nw3llpbSRDXA4nE6ma/LDRffzllhAPACtp3hT2VakCP\nfcMmTXaET3FlvzdUxDWDRo/eHm/M/u1dL8UJRgoh/z6ih83RAkEA60Qtzg0MpMjE\nWmTGEZeqiDX+HH1ZQ+N2fWgBZC+dqB8pP/GPxAh8HpS/Mtt8IcFYXaoyo0mLwVuL\nexQC6RyDpwJBAMrG7NPNQvj2blvNLrU8kyK1LSvqHoYXcvOR22tLhqHiGSKOuT5a\nXjFhWYgZm7M+P8ubBjNvPTCWi6oY4CrQE3kCQQDkuNnXMrSSF2VdhA9T1yFBX0x2\noh6Ac8kkTlLb9bbOVc0ij1P3f1A74tynMt7RakjgdrDYMo4eI0PNGj1iKAiNAkAf\nBKjriUWKYd/lyRAxBxAWyhIb2pdKucGKwrAGzKnOj5B6ucxaXmZ0NUkFya0IkSgf\nFBqxuX1ptk2s+lsoEWY5AkEAuS8PmzrkvJ/XNm1K7OFgdrrq+2pZ4KZ7h3aF6xWw\nmifDYh3jXTjNqIZsJg5gMsV5qy4izbSFgF0ENq1KTQY3aQ==\n-----END RSA PRIVATE KEY-----\n",
			cryData:    "cLjO01T+C/Ywp3f/9y4psYYjLv2EuI1xAfjux4Mk4te3gBWWqv1U3yuF5qWwagPggTFPRwN+ORwJwEPKr84VkW+CHoostQaIp9hcaeG54TvgZ+X9iWlw2JqE8sw9f0guMNSFpjCDhzZleikwE6QL08yF7s1mJ937PfCpSRIHR8g=",
			want:       "123456",
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			preDecode, err := base64.StdEncoding.DecodeString(tt.cryData)
			require.Nil(t, err)
			got, err := Decrypt([]byte(tt.privateKey), preDecode)
			if (err != nil) != tt.wantErr {
				t.Errorf("Decrypt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(string(got), tt.want) {
				t.Errorf("Decrypt() got = %v, want %v", got, tt.want)
			}
		})
	}
}
