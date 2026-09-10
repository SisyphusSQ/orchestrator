package main

import (
	"fmt"
	"io"

	"github.com/openark/orchestrator/internal/config"
	"github.com/spf13/cobra"
)

type commandOptions struct {
	configFile                              string
	discovery, quiet, verbose, debug, stack bool
	instance, destination, owner            string
	runtime                                 config.CLIFlags
}

func execute(args []string, stdout, stderr io.Writer, run func(*commandOptions, string) error) error {
	options := &commandOptions{}
	root := &cobra.Command{Use: "orchestrator", Short: "Run orchestrator services; use orch for remote operations", SilenceErrors: true, SilenceUsage: true, Version: AppVersion + "\n" + GitCommit, Args: cobra.NoArgs}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	root.SetVersionTemplate("{{.Version}}\n")
	flags := root.PersistentFlags()
	flags.StringVar(&options.configFile, "config", "", "Server configuration file")
	flags.BoolVar(&options.quiet, "quiet", false, "Only log errors")
	flags.BoolVar(&options.verbose, "verbose", false, "Log informational messages")
	flags.BoolVar(&options.debug, "debug", false, "Enable debug logging")
	flags.BoolVar(&options.stack, "stack", false, "Include error stack traces")
	options.runtime.Noop = flags.Bool("noop", false, "Server-wide dry run; do not change topology")
	options.runtime.SkipUnresolve = flags.Bool("skip-unresolve", false, "Do not unresolve hostnames")
	options.runtime.SkipUnresolveCheck = flags.Bool("skip-unresolve-check", false, "Skip unresolve consistency checks")
	options.runtime.SkipBinlogSearch = flags.Bool("skip-binlog-search", false, "Only search relay logs for Pseudo-GTID")
	options.runtime.EnableDatabaseUpdate = flags.Bool("enable-database-update", false, "Allow server schema updates")
	root.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	server := &cobra.Command{Use: "server", Short: "Run the Raft node and HTTP/Web services", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return run(options, "server") }}
	server.Flags().BoolVar(&options.discovery, "discovery", true, "Enable automatic topology discovery")
	root.AddCommand(server)
	admin := &cobra.Command{Use: "admin", Short: "Local server maintenance; requires server configuration", Args: cobra.NoArgs}
	for _, name := range []string{"dump-config", "redeploy-internal-db", "migrate-metadata-id", "access-token", "suggest-promoted-replacement"} {
		command := &cobra.Command{Use: name, Short: name, Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			if name == "access-token" && options.owner == "" {
				return fmt.Errorf("--owner is required")
			}
			return run(options, name)
		}}
		if name == "access-token" {
			command.Flags().StringVar(&options.owner, "owner", "", "Token owner")
		}
		if name == "suggest-promoted-replacement" {
			command.Hidden = true
			command.Flags().StringVarP(&options.instance, "instance", "i", "", "Failed instance")
			command.Flags().StringVarP(&options.destination, "destination", "d", "", "Promoted instance")
		}
		admin.AddCommand(command)
	}
	root.AddCommand(admin)
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	return root.Execute()
}
