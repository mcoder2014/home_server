// accounts-migrate produces a redacted offline migration plan by default.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mcoder2014/home_server/domain/dal/migrations"
	"github.com/mcoder2014/home_server/internal/accountsmigrate"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		json.NewEncoder(os.Stderr).Encode(map[string]string{"error": err.Error()})
		os.Exit(1)
	}
}

// run 默认生成脱敏迁移计划；应用模式先检查离线确认参数，再由 Apply 核对目标数据库和最新计划摘要。
// 可将报告写入新建文件，应用后重建计划核验完成标记；不负责停服务、备份、修改 YAML 或写入网页内容。
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("accounts-migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	conf := flags.String("config", "", "protected legacy YAML file (0600)")
	binary := flags.String("binary", "", "current deployed backend binary to fingerprint")
	database := flags.String("database", "", "optional isolated target database override; source credentials stay private")
	adminIDs := flags.String("admin-user-ids", "", "comma-separated initial administrator IDs (required)")
	libraryIDs := flags.String("library-user-ids", "", "comma-separated explicit library grants")
	readIDs := flags.String("webdav-read-user-ids", "", "comma-separated WebDAV read grants")
	writeIDs := flags.String("webdav-write-user-ids", "", "comma-separated WebDAV read/write grants")
	apply := flags.Bool("apply", false, "apply reviewed DDL and import; default is read-only plan")
	confirmed := flags.String("confirm-database", "", "exact target database name required for apply")
	planSHA := flags.String("plan-sha256", "", "reviewed plan digest required for apply")
	maintenance := flags.Bool("maintenance-confirmed", false, "confirm backend writers are stopped and fresh backups verified")
	output := flags.String("out", "", "write redacted JSON to a new mode-0600 file instead of stdout")
	timeout := flags.Duration("timeout", 10*time.Minute, "total database/migration timeout")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid command-line arguments")
	}
	if flags.NArg() != 0 || *conf == "" || *binary == "" {
		return errors.New("--config and --binary are required; no positional arguments are accepted")
	}
	if *timeout <= 0 || *timeout > time.Hour {
		return errors.New("timeout must be greater than zero and at most one hour")
	}
	if *apply && (!*maintenance || *confirmed == "" || *planSHA == "") {
		return errors.New("apply requires --maintenance-confirmed, --confirm-database and --plan-sha256")
	}
	grants := accountsmigrate.Grants{}
	for _, pair := range []struct {
		raw    string
		target *[]int64
	}{{*adminIDs, &grants.Admins}, {*libraryIDs, &grants.Library}, {*readIDs, &grants.WebDAVRead}, {*writeIDs, &grants.WebDAVWrite}} {
		ids, err := parseIDs(pair.raw)
		if err != nil {
			return err
		}
		*pair.target = ids
	}
	source, err := accountsmigrate.ReadSource(*conf, grants)
	if err != nil {
		return err
	}
	binaryFile, err := os.Open(*binary)
	if err != nil {
		return errors.New("cannot open deployment binary for fingerprinting")
	}
	info, err := binaryFile.Stat()
	if err != nil || !info.Mode().IsRegular() {
		binaryFile.Close()
		return errors.New("deployment binary must be a regular file")
	}
	binaryHash := sha256.New()
	_, err = io.Copy(binaryHash, binaryFile)
	binaryFile.Close()
	if err != nil {
		return errors.New("cannot fingerprint deployment binary")
	}
	opts := accountsmigrate.Options{BinarySHA256: hex.EncodeToString(binaryHash.Sum(nil)), DDLChecksums: map[string]string{}, Now: time.Now()}
	for _, name := range []string{"20260913_runtime_config.sql", "20260913_accounts.sql"} {
		raw, err := migrations.SQL.ReadFile(name)
		if err != nil {
			return errors.New("required embedded migration is missing")
		}
		sum := sha256.Sum256(raw)
		opts.DDLChecksums[name] = hex.EncodeToString(sum[:])
		steps, err := accountsmigrate.ParseDDL(name, raw)
		if err != nil {
			return fmt.Errorf("embedded migration %s is unsupported: %w", name, err)
		}
		opts.Steps = append(opts.Steps, steps...)
	}
	db, name, err := accountsmigrate.OpenDatabase(source, *database)
	if err != nil {
		return err
	}
	defer db.Close()
	opts.Database = name
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	plan, err := accountsmigrate.BuildPlan(ctx, db, source, opts)
	if err != nil {
		return err
	}
	if *output != "" {
		file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("cannot create new output file; existing files are never overwritten")
		}
		defer file.Close()
		stdout = file
	}
	action := "plan"
	if *apply {
		if err := accountsmigrate.Apply(ctx, db, source, opts, *planSHA, *confirmed); err != nil {
			return err
		}
		plan, err = accountsmigrate.BuildPlan(ctx, db, source, opts)
		if err != nil {
			return fmt.Errorf("migration applied but readback verification failed: %w", err)
		}
		if !plan.DataAlreadyImported {
			return errors.New("migration applied but completion marker is missing")
		}
		action = "applied_and_verified"
	}
	result := struct {
		Action string                `json:"action"`
		Plan   *accountsmigrate.Plan `json:"plan"`
	}{action, plan}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return errors.New("cannot write migration report")
	}
	return nil
}

func parseIDs(raw string) ([]int64, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var ids []int64
	for _, value := range strings.Split(raw, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || id <= 0 {
			return nil, errors.New("grant IDs must be positive comma-separated integers")
		}
		ids = append(ids, id)
	}
	return ids, nil
}
