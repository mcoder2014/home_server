// Package acceleration owns the optional Redis clients and statistics lifecycle.
package acceleration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/dal"
	"github.com/mcoder2014/home_server/domain/service/webanalytics"
	"github.com/mcoder2014/home_server/utils/cache"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type Runtime struct {
	Redis      *redis.Client
	Analytics  *webanalytics.Service
	cache      *cache.Client
	visitorKey []byte
	cancel     context.CancelFunc
}

var Current atomic.Pointer[Runtime]

func newRedisClient(conf config.Config, password []byte) *redis.Client {
	timeout := time.Duration(conf.Redis.CommandTimeoutMS) * time.Millisecond
	if conf.Analytics.Enabled && timeout < webanalytics.DefaultOperationTimeout {
		timeout = webanalytics.DefaultOperationTimeout
	}
	return redis.NewClient(&redis.Options{
		Addr: conf.Redis.Address, DB: conf.Redis.DB, Username: conf.Redis.Username, Password: strings.TrimSpace(string(password)),
		DialTimeout: time.Duration(conf.Redis.DialTimeoutMS) * time.Millisecond,
		ReadTimeout: timeout, WriteTimeout: timeout, PoolTimeout: timeout,
		PoolSize: conf.Redis.PoolSize, MaxRetries: 1, ContextTimeoutEnabled: true,
	})
}

// Initialize creates disposable clients without changing server configuration or
// applying DDL. Unreachable Redis cannot block ordinary site startup; the cache
// falls back and the statistics worker reports its own dependency failures.
func Initialize(conf config.Config, database *gorm.DB) (*Runtime, error) {
	if err := config.NormalizeInfrastructure(&conf); err != nil {
		return nil, err
	}
	result := &Runtime{}
	if conf.Redis.Enabled {
		password := []byte(nil)
		var err error
		if conf.Redis.PasswordFile != "" {
			password, err = readSecret(conf.Redis.PasswordFile, 1)
			if err != nil {
				return nil, fmt.Errorf("Redis credential file: %w", err)
			}
		}
		result.Redis = newRedisClient(conf, password)
		if conf.Cache.Enabled {
			result.cache = cache.New(result.Redis, conf.Redis.KeyPrefix)
		}
	}
	if conf.Analytics.Enabled {
		key, err := readSecret(conf.Analytics.VisitorHMACKeyFile, 32)
		if err != nil {
			result.Redis.Close()
			return nil, fmt.Errorf("analytics visitor key file: %w", err)
		}
		result.visitorKey = key
		result.Analytics, err = webanalytics.New(result.Redis, database, webanalytics.Options{
			Prefix: conf.Redis.KeyPrefix, Timezone: conf.Analytics.Timezone,
			RetentionDays: conf.Analytics.RetentionDays, HotDays: conf.Analytics.HotDays,
			QueueSize: conf.Analytics.QueueSize, FlushInterval: time.Duration(conf.Analytics.FlushIntervalSeconds) * time.Second,
		})
		if err != nil {
			result.Redis.Close()
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	result.cancel = cancel
	dal.ConfigureReadCache(result.cache, conf.Cache.Namespaces)
	Current.Store(result)
	if result.Analytics != nil {
		result.Analytics.Start(ctx)
	}
	if result.Redis != nil {
		go result.report(ctx)
	}
	return result, nil
}

// VisitorHash is project-scoped and never reveals the browser identifier. The
// key is unrelated to authentication and remains fixed for this statistics era.
func (r *Runtime) VisitorHash(projectID int64, visitorID string) string {
	mac := hmac.New(sha256.New, r.visitorKey)
	mac.Write([]byte(strconv.FormatInt(projectID, 10)))
	mac.Write([]byte{0})
	mac.Write([]byte(visitorID))
	return hex.EncodeToString(mac.Sum(nil))
}

// Close stops event admission before waiting for a bounded final snapshot. It
// never clears Redis keys or drops the persistent statistics tables.
func (r *Runtime) Close(ctx context.Context) error {
	Current.CompareAndSwap(r, nil)
	if r.cancel != nil {
		r.cancel()
	}
	var err error
	if r.Analytics != nil {
		err = r.Analytics.Close(ctx)
	}
	dal.ConfigureReadCache(nil, nil)
	if r.Redis != nil {
		err = errors.Join(err, r.Redis.Close())
	}
	return err
}

func (r *Runtime) report(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fields := logrus.Fields{}
			if r.cache != nil {
				fields["cache"] = r.cache.Stats()
			}
			if r.Analytics != nil {
				fields["analytics"] = r.Analytics.Health()
			}
			logrus.WithFields(fields).Info("Redis cache and analytics health")
		}
	}
}

// readSecret rejects symlinks and non-private or oversized files. O_NOFOLLOW and
// descriptor metadata close the path-swap window between validation and reading.
func readSecret(path string, minimum int) ([]byte, error) {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("cannot open a private regular file")
	}
	file := os.NewFile(uintptr(descriptor), "private secret")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return nil, errors.New("secret must be a private regular file of at most 4096 bytes")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) < minimum || len(data) > 4096 {
		return nil, errors.New("secret has an invalid size")
	}
	return data, nil
}
