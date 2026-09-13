package main

import (
	"encoding/json"
	"flag"
	"os"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/service/webprojects"
)

type commandOutput struct {
	OK     bool                                `json:"ok"`
	Report *webprojects.StorageMigrationReport `json:"report,omitempty"`
	Error  string                              `json:"error,omitempty"`
}

func main() {
	configPath := flag.String("conf", "/etc/home_server/conf.yaml", "home_server config file")
	projectID := flag.Int64("project-id", 0, "legacy web project id")
	releaseID := flag.Int64("release-id", 0, "legacy web project release id")
	apply := flag.Bool("apply", false, "apply the migration; omitted means dry-run")
	flag.Parse()

	if *projectID <= 0 || *releaseID <= 0 {
		writeOutput(commandOutput{Error: "project-id and release-id must be positive"}, 2)
	}
	if err := config.InitGlobalConfig(*configPath); err != nil {
		writeOutput(commandOutput{Error: "configuration unavailable"}, 1)
	}
	conf := config.Global()
	if err := db.InitDatabase(conf.Mysql.MasterDB); err != nil {
		writeOutput(commandOutput{Error: "database unavailable"}, 1)
	}
	report, err := webprojects.MigrateLegacyReleaseStorage(&conf.WebProjects, *projectID, *releaseID, *apply)
	if err != nil {
		writeOutput(commandOutput{Report: report, Error: err.Error()}, 1)
	}
	writeOutput(commandOutput{OK: true, Report: report}, 0)
}

func writeOutput(output commandOutput, exitCode int) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(output); err != nil {
		os.Exit(1)
	}
	os.Exit(exitCode)
}
