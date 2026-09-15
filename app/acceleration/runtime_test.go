package acceleration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/service/webanalytics"
)

func TestRedisTransportTimeoutCoversEnabledWorkloads(t *testing.T) {
	cases := []struct {
		name      string
		analytics bool
		want      time.Duration
	}{
		{name: "cache only", want: 20 * time.Millisecond},
		{name: "analytics", analytics: true, want: webanalytics.DefaultOperationTimeout},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			conf := config.Config{}
			conf.Redis.Enabled = true
			conf.Redis.CommandTimeoutMS = 20
			conf.Analytics.Enabled = item.analytics
			conf.Analytics.VisitorHMACKeyFile = "/private/test-analytics-key"
			if err := config.NormalizeInfrastructure(&conf); err != nil {
				t.Fatal(err)
			}
			client := newRedisClient(conf, nil)
			t.Cleanup(func() { _ = client.Close() })
			options := client.Options()
			if options.ReadTimeout < item.want || options.WriteTimeout < item.want || options.PoolTimeout < item.want {
				t.Fatalf("redis timeouts read=%v write=%v pool=%v, want at least %v", options.ReadTimeout, options.WriteTimeout, options.PoolTimeout, item.want)
			}
		})
	}
}

func TestReadSecretRejectsUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "secret")
	if err := os.WriteFile(path, []byte("01234567890123456789012345678901"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := readSecret(path, 32)
	if err != nil || len(data) != 32 {
		t.Fatalf("private key not accepted: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecret(link, 32); err == nil {
		t.Fatal("symbolic link accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecret(path, 32); err == nil {
		t.Fatal("world-readable secret accepted")
	}
	if err := os.WriteFile(path, []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecret(path, 32); err == nil {
		t.Fatal("short key accepted")
	}
}
