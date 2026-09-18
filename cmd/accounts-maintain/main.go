// accounts-maintain performs reviewed account-center DDL and bounded retention cleanup.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mcoder2014/home_server/internal/accountsmigrate"
	"gopkg.in/yaml.v3"
)

// run defaults to a redacted schema-only plan. Offline DDL requires all three
// reviewed confirmations, while daily retention cleanup is an explicit separate
// mode. It accepts ordinary protected server YAML without legacy mock identities;
// no configuration contents or server-supplied connection errors are printed.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("accounts-maintain", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	conf := flags.String("config", "", "protected server YAML file (0600)")
	database := flags.String("database", "", "optional isolated database override")
	apply := flags.Bool("apply-migration", false, "apply reviewed account center DDL during maintenance")
	clear := flags.Bool("clear-session-metadata", false, "erase metadata thirty days after natural session expiry")
	confirmed := flags.String("confirm-database", "", "exact target database required for migration apply")
	planSHA := flags.String("plan-sha256", "", "reviewed additive plan digest required for migration apply")
	maintenance := flags.Bool("maintenance-confirmed", false, "confirm backend writers stopped and fresh backup verified")
	output := flags.String("out", "", "write redacted JSON to a new mode-0600 file")
	timeout := flags.Duration("timeout", 5*time.Minute, "total timeout; cleanup is additionally bounded to five minutes")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(stderr)
			flags.PrintDefaults()
			return nil
		}
		return errors.New("invalid command-line arguments (details suppressed)")
	}
	if flags.NArg() != 0 || *conf == "" {
		return errors.New("--config is required; no positional arguments are accepted")
	}
	if *apply && *clear {
		return errors.New("--apply-migration and --clear-session-metadata are separate modes")
	}
	if *timeout <= 0 || *timeout > time.Hour {
		return errors.New("timeout must be greater than zero and at most one hour")
	}
	if *apply && (!*maintenance || *confirmed == "" || *planSHA == "") {
		return errors.New("apply-migration requires --maintenance-confirmed, --confirm-database and --plan-sha256")
	}
	source, err := readConfig(*conf)
	if err != nil {
		return err
	}
	db, name, err := accountsmigrate.OpenDatabase(source, *database)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	if *output != "" {
		file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("cannot create new output file; existing files are never overwritten")
		}
		defer file.Close()
		stdout = file
	}
	if *clear {
		result, err := accountsmigrate.ClearSessionMetadata(ctx, db, name, time.Now())
		if err != nil {
			return err
		}
		return writeReport(stdout, map[string]interface{}{"action": "session_metadata_cleared", "result": result})
	}
	opts, err := accountsmigrate.AccountCenterOptions(name)
	if err != nil {
		return err
	}
	plan, err := accountsmigrate.BuildAdditivePlan(ctx, db, opts)
	if err != nil {
		return err
	}
	action := "migration_plan"
	if *apply {
		if err := accountsmigrate.ApplyAdditive(ctx, db, opts, *planSHA, *confirmed, *maintenance); err != nil {
			return err
		}
		plan, err = accountsmigrate.BuildAdditivePlan(ctx, db, opts)
		if err != nil {
			return fmt.Errorf("migration applied but readback verification failed: %w", err)
		}
		if len(plan.PendingSteps) != 0 || len(plan.PendingMarkers) != 0 {
			return errors.New("migration applied but schema or stage markers remain incomplete")
		}
		action = "migration_applied_and_verified"
	}
	return writeReport(stdout, map[string]interface{}{"action": action, "plan": plan})
}

func readConfig(path string) (*accountsmigrate.Source, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("cannot stat protected server config")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0177 != 0 {
		return nil, errors.New("server config must be a regular file with mode 0600 or stricter")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read protected server config")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm()&0177 != 0 {
		return nil, errors.New("protected server config changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4*1024*1024+1))
	if err != nil || len(raw) > 4*1024*1024 {
		return nil, errors.New("cannot read server config within the four MiB limit")
	}
	source := &accountsmigrate.Source{}
	if err := yaml.Unmarshal(raw, &source.Config); err != nil {
		return nil, errors.New("invalid server YAML (details suppressed)")
	}
	if source.Config.Mysql.MasterDB == "" {
		return nil, errors.New("server config requires mysql.master_db")
	}
	return source, nil
}

func writeReport(output io.Writer, report interface{}) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return errors.New("cannot write redacted maintenance report")
	}
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		json.NewEncoder(os.Stderr).Encode(map[string]string{"error": err.Error()})
		os.Exit(1)
	}
}
