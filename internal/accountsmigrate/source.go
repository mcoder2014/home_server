// Package accountsmigrate implements the explicit, offline account/config cutover.
// It never starts the HTTP service or changes deployment files.
package accountsmigrate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/config"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

type Grants struct {
	Admins      []int64 `json:"admins"`
	Library     []int64 `json:"library"`
	WebDAVRead  []int64 `json:"webdav_read"`
	WebDAVWrite []int64 `json:"webdav_write"`
}

type LegacyUser struct {
	ID               int64     `json:"id"`
	Username         string    `json:"user_name"`
	PasswordHash     string    `json:"password"`
	Email            string    `json:"email"`
	Mobile           string    `json:"mobile"`
	CreateTime       time.Time `json:"create_time"`
	UpdateTime       time.Time `json:"update_time"`
	Role             string    `json:"-"`
	WebDAVPermission string    `json:"-"`
	LibraryEnabled   bool      `json:"-"`
}

type Alias struct {
	UserID    int64
	Key, Kind string
}
type Source struct {
	Config  config.Config `json:"-"`
	Users   []LegacyUser  `json:"-"`
	Aliases []Alias       `json:"-"`
	SHA256  string        `json:"source_sha256"`
	Grants  Grants        `json:"grants"`
}

// ReadSource deliberately rejects symlinks and group/world-readable secret files.
func ReadSource(path string, grants Grants) (*Source, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("cannot stat protected source config")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("source config must be a regular file with mode 0600 or stricter")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("cannot read protected source config")
	}
	return ParseSource(raw, grants)
}

func ParseSource(raw []byte, grants Grants) (*Source, error) {
	source := &Source{Grants: grants}
	if err := yaml.Unmarshal(raw, &source.Config); err != nil {
		return nil, errors.New("invalid source YAML (details suppressed)")
	}
	if err := json.Unmarshal([]byte(source.Config.Passport.MockData), &source.Users); err != nil {
		return nil, errors.New("invalid legacy identity JSON (details suppressed)")
	}
	if len(source.Users) == 0 {
		return nil, errors.New("source contains no legacy users")
	}
	seenIDs, aliases := map[int64]bool{}, map[string]int64{}
	for i := range source.Users {
		user := &source.Users[i]
		if user.ID <= 0 || seenIDs[user.ID] {
			return nil, fmt.Errorf("invalid or duplicate source user ID at row %d", i+1)
		}
		if user.Username == "" || strings.TrimSpace(user.Username) != user.Username || utf8.RuneCountInString(user.Username) > 64 {
			return nil, fmt.Errorf("invalid source username at row %d", i+1)
		}
		if _, err := bcrypt.Cost([]byte(user.PasswordHash)); err != nil {
			return nil, fmt.Errorf("invalid bcrypt password at user ID %d", user.ID)
		}
		if utf8.RuneCountInString(user.Email) > 254 || utf8.RuneCountInString(user.Mobile) > 32 {
			return nil, fmt.Errorf("source contact exceeds column limit at user ID %d", user.ID)
		}
		if (!user.CreateTime.IsZero() && user.CreateTime.Year() < 1000) || (!user.UpdateTime.IsZero() && user.UpdateTime.Year() < 1000) {
			return nil, fmt.Errorf("unsupported legacy date at user ID %d", user.ID)
		}
		seenIDs[user.ID] = true
		user.Role, user.WebDAVPermission = "user", "none"
		for _, pair := range [][2]string{{"username", user.Username}, {"email", user.Email}, {"mobile", user.Mobile}} {
			key := strings.ToLower(strings.TrimSpace(pair[1]))
			if key == "" {
				continue
			}
			if utf8.RuneCountInString(key) > 254 {
				return nil, fmt.Errorf("source login alias too long at user ID %d", user.ID)
			}
			if owner, exists := aliases[key]; exists {
				if owner != user.ID {
					return nil, fmt.Errorf("login alias collision between user IDs %d and %d", owner, user.ID)
				}
				continue
			}
			aliases[key] = user.ID
			source.Aliases = append(source.Aliases, Alias{UserID: user.ID, Key: key, Kind: pair[0]})
		}
	}
	if err := applyGrants(source, seenIDs); err != nil {
		return nil, err
	}
	sort.Slice(source.Users, func(i, j int) bool { return source.Users[i].ID < source.Users[j].ID })
	sort.Slice(source.Aliases, func(i, j int) bool { return source.Aliases[i].Key < source.Aliases[j].Key })
	hash := sha256.Sum256(raw)
	source.SHA256 = hex.EncodeToString(hash[:])
	return source, nil
}

func applyGrants(source *Source, known map[int64]bool) error {
	if len(source.Grants.Admins) == 0 {
		return errors.New("at least one explicit initial admin ID is required")
	}
	sets := make([]map[int64]bool, 4)
	for i, ids := range [][]int64{source.Grants.Admins, source.Grants.Library, source.Grants.WebDAVRead, source.Grants.WebDAVWrite} {
		sets[i] = map[int64]bool{}
		for _, id := range ids {
			if !known[id] || sets[i][id] {
				return fmt.Errorf("unknown or duplicate grant user ID %d", id)
			}
			sets[i][id] = true
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	}
	for i := range source.Users {
		user := &source.Users[i]
		if sets[2][user.ID] && sets[3][user.ID] {
			return fmt.Errorf("conflicting WebDAV grants for user ID %d", user.ID)
		}
		if sets[0][user.ID] {
			user.Role = "admin"
		}
		user.LibraryEnabled = sets[1][user.ID]
		if sets[2][user.ID] {
			user.WebDAVPermission = "read"
		}
		if sets[3][user.ID] {
			user.WebDAVPermission = "write"
		}
	}
	return nil
}
