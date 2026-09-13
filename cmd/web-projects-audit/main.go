package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/db"
	"github.com/mcoder2014/home_server/domain/service/webprojects"
)

type commandOutput struct {
	OK     bool                            `json:"ok"`
	Report *webprojects.StorageAuditReport `json:"report,omitempty"`
	Error  string                          `json:"error,omitempty"`
}

func main() {
	configPath := flag.String("conf", "/etc/home_server/conf.yaml", "home_server config file")
	minAge := flag.Duration("min-age", time.Hour, "ignore release directories newer than this duration")
	flag.Parse()

	if *minAge < 0 {
		writeOutput(commandOutput{Error: "min-age must not be negative"}, 2)
	}
	if err := config.InitGlobalConfig(*configPath); err != nil {
		writeOutput(commandOutput{Error: "configuration unavailable"}, 1)
	}
	conf := config.Global()
	if err := db.InitDatabase(conf.Mysql.MasterDB); err != nil {
		writeOutput(commandOutput{Error: "database unavailable"}, 1)
	}
	report, err := webprojects.AuditStorage(&conf.WebProjects, *minAge)
	if err != nil {
		if errors.Is(err, webprojects.ErrAuditDatabase) {
			writeOutput(commandOutput{Error: "database query failed"}, 1)
		}
		writeOutput(commandOutput{Error: "storage audit failed"}, 1)
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
