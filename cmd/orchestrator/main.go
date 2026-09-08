/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/openark/orchestrator/internal/app"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/db"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst"
	"github.com/openark/orchestrator/internal/logic"
	"github.com/openark/orchestrator/internal/observability"
	"github.com/openark/orchestrator/internal/process"
)

var AppVersion, GitCommit string

// main is the application's entry point. It will either spawn a CLI or HTTP interfaces.
func main() {
	log.RegisterCloseHook(app.CloseRaftRuntime)
	log.RegisterCloseHook(app.CloseHealthMonitor)
	registerProcessCloseHooks(log.RegisterCloseHook, inst.CloseAuditSyslog, db.Close)
	exitCode := run()
	if err := log.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "logger close failed: %v\n", err)
		exitCode = 1
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func registerProcessCloseHooks(
	register func(func() error),
	closeAuditSyslog func() error,
	closeDatabaseRuntime func() error,
) {
	register(func() error {
		if err := closeAuditSyslog(); err != nil {
			return fmt.Errorf("close audit syslog: %w", err)
		}
		return nil
	})
	register(func() error {
		if err := closeDatabaseRuntime(); err != nil {
			return fmt.Errorf("close database runtime: %w", err)
		}
		return nil
	})
}

func run() int {
	if err := execute(os.Args[1:], os.Stdout, os.Stderr, runCommand); err != nil {
		log.Errorf("%v", err)
		return 1
	}
	return 0
}

func runCommand(options *commandOptions, command string) error {
	if err := process.HostnameError(); err != nil {
		return err
	}
	config.RuntimeCLIFlags = options.runtime
	log.SetLevel(log.ERROR)
	if options.verbose {
		log.SetLevel(log.INFO)
	}
	if options.debug {
		log.SetLevel(log.DEBUG)
	}
	log.SetPrintStackTrace(options.stack)
	startText := "starting orchestrator"
	if AppVersion != "" {
		startText += ", version: " + AppVersion
	}
	if GitCommit != "" {
		startText += ", git commit: " + GitCommit
	}
	log.Info(startText)

	var configErr error
	if len(options.configFile) > 0 {
		_, configErr = config.ForceRead(options.configFile)
	} else {
		_, configErr = config.Read("/etc/orchestrator.conf.json", "conf/orchestrator.conf.json", "orchestrator.conf.json")
	}
	if configErr != nil {
		return fmt.Errorf("load configuration: %w", configErr)
	}
	if *config.RuntimeCLIFlags.EnableDatabaseUpdate {
		config.Config.SkipOrchestratorDatabaseUpdate = false
	}
	if config.Config.Debug {
		log.SetLevel(log.DEBUG)
	}
	if options.quiet {
		// Override!!
		log.SetLevel(log.ERROR)
	}
	if err := configureSyslog(config.Config.EnableSyslog, log.EnableSyslogWriter); err != nil {
		return err
	}
	if config.Config.AuditToSyslog {
		if err := inst.EnableAuditSyslog(); err != nil {
			return fmt.Errorf("initialize audit syslog: %w", err)
		}
	}
	config.RuntimeCLIFlags.ConfiguredVersion = AppVersion
	telemetry, err := observability.New(context.Background(), config.Config.OTelTraceEndpoint, config.Config.OTelTraceSampleRatio, AppVersion)
	if err != nil {
		return fmt.Errorf("initialize telemetry: %w", err)
	}
	telemetry.Install()
	log.RegisterCloseHook(telemetry.Close)
	config.MarkConfigurationLoaded()

	if command == "server" {
		if err := app.Http(options.discovery); err != nil {
			return fmt.Errorf("run HTTP services: %w", err)
		}
		return nil
	}
	switch command {
	case "dump-config":
		fmt.Println(config.Config.ToJSONString())
		return nil
	case "redeploy-internal-db":
		config.RuntimeCLIFlags.ConfiguredVersion = ""
		_, err := inst.ReadClusters()
		return err
	case "access-token":
		token, err := process.GenerateAccessToken(options.owner)
		if err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	case "suggest-promoted-replacement":
		key, err := inst.ParseRawInstanceKey(options.instance)
		if err != nil {
			return err
		}
		destination, err := inst.ParseRawInstanceKey(options.destination)
		if err != nil {
			return err
		}
		instance, found, err := inst.ReadInstance(destination)
		if err != nil {
			return err
		}
		if !found || instance == nil {
			return fmt.Errorf("destination not found")
		}
		result, _, err := logic.SuggestReplacementForPromotedReplica(&logic.TopologyRecovery{}, key, instance, nil)
		if err != nil {
			return err
		}
		if result == nil {
			return fmt.Errorf("replacement not found")
		}
		fmt.Println(result.Key.DisplayString())
		return nil
	}
	return fmt.Errorf("unknown server operation")
}

func configureSyslog(enabled bool, enable func(string) error) error {
	if !enabled {
		return nil
	}
	if err := enable("orchestrator"); err != nil {
		return fmt.Errorf("initialize syslog: %w", err)
	}
	log.SetSyslogLevel(log.INFO)
	return nil
}
