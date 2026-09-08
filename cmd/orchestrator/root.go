package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/openark/orchestrator/internal/config"
)

// commandOptions 只属于一次命令调用，解析阶段不改写进程运行时参数。
type commandOptions struct {
	configFile   string
	command      string
	strict       bool
	instance     string
	sibling      string
	destination  string
	owner        string
	reason       string
	duration     string
	pattern      string
	clusterAlias string
	pool         string
	hostnameFlag string
	discovery    bool
	quiet        bool
	verbose      bool
	debug        bool
	stack        bool
	runtime      config.CLIFlags
}

func execute(args []string, stdout, stderr io.Writer, run func(*commandOptions, string) error) error {
	root := newRootCommand(run)
	root.SetOut(stdout)
	root.SetErr(stderr)
	// 补全生成器会捕获输出目标，因此必须在设置 writer 后初始化。
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	setCommandVersions(root, root.Version)
	normalized, err := normalizeLegacyFlags(args, root.PersistentFlags())
	if err != nil {
		return err
	}
	root.SetArgs(normalized)
	return root.Execute()
}

func setCommandVersions(command *cobra.Command, version string) {
	command.Version = version
	for _, child := range command.Commands() {
		setCommandVersions(child, version)
	}
}

func newRootCommand(run func(*commandOptions, string) error) *cobra.Command {
	options := &commandOptions{}
	root := &cobra.Command{
		Use:           "orchestrator",
		Short:         "Manage MySQL replication topologies",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		Version:       AppVersion + "\n" + GitCommit,
		Example:       "  orchestrator clusters\n  orchestrator discover -i db.example.com:3306\n  orchestrator http --config=/path/to/config.json\n  orchestrator -c clusters",
	}
	root.SetVersionTemplate("{{.Version}}\n")
	flags := root.PersistentFlags()
	// Go flag 接受 --c/--i/--s/--d，迁移后仍映射到对应的长参数。
	root.SetGlobalNormalizationFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		switch name {
		case "c":
			name = "command"
		case "i":
			name = "instance"
		case "s":
			name = "sibling"
		case "d":
			name = "destination"
		}
		return pflag.NormalizedName(name)
	})
	flags.StringVar(&options.configFile, "config", "", "config file name")
	flags.StringVarP(&options.command, "command", "c", "", "Legacy command selector; use a subcommand or -c, not both")
	flags.BoolVar(&options.strict, "strict", false, "strict mode (more checks, slower)")
	flags.StringVarP(&options.instance, "instance", "i", "", "instance, host_fqdn[:port] (e.g. db.company.com:3306, db.company.com)")
	flags.StringVarP(&options.sibling, "sibling", "s", "", "sibling instance, host_fqdn[:port]")
	flags.StringVarP(&options.destination, "destination", "d", "", "destination instance, host_fqdn[:port] (synonym to -s)")
	flags.StringVar(&options.owner, "owner", "", "operation owner")
	flags.StringVar(&options.reason, "reason", "", "operation reason")
	flags.StringVar(&options.duration, "duration", "", "maintenance duration (format: 59s, 59m, 23h, 6d, 4w)")
	flags.StringVar(&options.pattern, "pattern", "", "regular expression pattern")
	flags.StringVar(&options.clusterAlias, "alias", "", "cluster alias")
	flags.StringVar(&options.pool, "pool", "", "Pool logical name (applies for pool-related commands)")
	flags.StringVar(&options.hostnameFlag, "hostname", "", "Hostname/fqdn/CNAME/VIP (applies for hostname/resolve related commands)")
	flags.BoolVar(&options.discovery, "discovery", true, "auto discovery mode")
	flags.BoolVar(&options.quiet, "quiet", false, "quiet")
	flags.BoolVar(&options.verbose, "verbose", false, "verbose")
	flags.BoolVar(&options.debug, "debug", false, "debug mode (very verbose)")
	flags.BoolVar(&options.stack, "stack", false, "add stack trace upon error")
	options.runtime.SkipBinlogSearch = flags.Bool("skip-binlog-search", false, "when matching via Pseudo-GTID, only use relay logs. This can save the hassle of searching for a non-existend pseudo-GTID entry, for example in servers with replication filters.")
	options.runtime.SkipUnresolve = flags.Bool("skip-unresolve", false, "Do not unresolve a host name")
	options.runtime.SkipUnresolveCheck = flags.Bool("skip-unresolve-check", false, "Skip/ignore checking an unresolve mapping (via hostname_unresolve table) resolves back to same hostname")
	options.runtime.Noop = flags.Bool("noop", false, "Dry run; do not perform destructing operations")
	options.runtime.BinlogFile = flags.String("binlog", "", "Binary log file name")
	options.runtime.Statement = flags.String("statement", "", "Statement/hint")
	options.runtime.GrabElection = flags.Bool("grab-election", false, "Grab leadership (only applies to continuous mode)")
	options.runtime.PromotionRule = flags.String("promotion-rule", "prefer", "Promotion rule for register-andidate (prefer|neutral|prefer_not|must_not)")
	options.runtime.Version = flags.Bool("version", false, "Print version and exit")
	options.runtime.SkipContinuousRegistration = flags.Bool("skip-continuous-registration", false, "Skip cli commands performaing continuous registration (to reduce orchestratrator backend db load")
	options.runtime.EnableDatabaseUpdate = flags.Bool("enable-database-update", false, "Enable database update, overrides SkipOrchestratorDatabaseUpdate")
	options.runtime.IgnoreRaftSetup = flags.Bool("ignore-raft-setup", false, "Override RaftEnabled for CLI invocation (CLI by default not allowed for raft setups). NOTE: operations by CLI invocation may not reflect in all raft nodes.")
	options.runtime.Tag = flags.String("tag", "", "tag to add ('tagname' or 'tagname=tagvalue') or to search ('tagname' or 'tagname=tagvalue' or comma separated 'tag0,tag1=val1,tag2' for intersection of all)")
	flags.BoolP("help", "h", false, "Show help without initializing runtime services")
	root.PersistentPreRunE = func(command *cobra.Command, _ []string) error {
		if flags.Changed("command") && command != root && command.Name() != "cli" && command.Name() != "help" {
			return fmt.Errorf("cannot combine -c/--command with subcommand %q", command.Name())
		}
		return nil
	}

	businessCommands := map[string]*cobra.Command{}
	groups := map[string]bool{}
	for _, definition := range commandDefinitions() {
		section := definition.Section
		if section == "" {
			section = "Other operations"
		}
		if !groups[section] {
			root.AddGroup(&cobra.Group{ID: section, Title: section + ":"})
			groups[section] = true
		}
		command := &cobra.Command{
			Use:     definition.Command,
			Short:   definition.Description,
			Long:    commandHelp[definition.Command],
			Aliases: definition.Aliases,
			GroupID: section,
			Args:    cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				if err := options.validate(); err != nil {
					return err
				}
				return run(options, command.Name())
			},
		}
		root.AddCommand(command)
		businessCommands[definition.Command] = command
		for _, alias := range definition.Aliases {
			businessCommands[alias] = command
		}
	}
	root.AddGroup(&cobra.Group{ID: "execution", Title: "Execution and help:"})
	root.SetHelpCommandGroupID("execution")
	root.SetCompletionCommandGroupID("execution")
	root.RunE = func(_ *cobra.Command, _ []string) error {
		if options.command == "help" || !flags.Changed("command") {
			return root.Help()
		}
		command, ok := businessCommands[options.command]
		if !ok {
			return fmt.Errorf("unknown command %q", options.command)
		}
		return command.RunE(command, nil)
	}
	root.AddCommand(&cobra.Command{
		Use:     "cli",
		Short:   "Legacy -c command entry point",
		Hidden:  true,
		Args:    cobra.NoArgs,
		RunE:    root.RunE,
		GroupID: "execution",
	})
	root.AddCommand(&cobra.Command{
		Use:     "http",
		Short:   "Run the HTTP services and optional discovery",
		GroupID: "execution",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := options.validate(); err != nil {
				return err
			}
			return run(options, "http")
		},
	})
	root.SetHelpCommand(&cobra.Command{
		Use:     "help [command]",
		Short:   "Show command help without initializing runtime services",
		GroupID: "execution",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			topic := options.command
			if len(args) > 0 {
				topic = args[0]
			}
			if topic == "" || topic == "help" || topic == "cli" {
				return root.Help()
			}
			if command, ok := businessCommands[topic]; ok {
				return command.Help()
			}
			for _, command := range root.Commands() {
				if command.Name() == topic {
					return command.Help()
				}
			}
			return fmt.Errorf("unknown help topic %q", topic)
		},
	})
	return root
}

func (options *commandOptions) validate() error {
	if options.destination != "" && options.sibling != "" {
		return fmt.Errorf("-s and -d are synonyms, yet both were specified")
	}
	switch *options.runtime.PromotionRule {
	case "prefer", "neutral", "prefer_not", "must_not":
	default:
		return fmt.Errorf("--promotion-rule only supports prefer|neutral|prefer_not|must_not")
	}
	if options.destination == "" {
		options.destination = options.sibling
	}
	return nil
}

// normalizeLegacyFlags 仅改写已注册的单横线长参数；参数值和 -- 后的内容保持原样。
// 不接受短参数合并或紧贴值，避免把拼错的旧长参数解释成另一个操作参数。
func normalizeLegacyFlags(args []string, flags *pflag.FlagSet) ([]string, error) {
	result := make([]string, 0, len(args))
	completionRequest := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// 补全协议的最后一项是尚未输入完整的词，不能按完整参数校验。
		if completionRequest && i == len(args)-1 {
			return append(result, arg), nil
		}
		if arg == "--" {
			return append(result, args[i:]...), nil
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			if arg == cobra.ShellCompRequestCmd || arg == cobra.ShellCompNoDescRequestCmd {
				completionRequest = true
			}
			result = append(result, arg)
			continue
		}
		name, _, hasValue := strings.Cut(strings.TrimPrefix(arg, "-"), "=")
		var flag *pflag.Flag
		if strings.HasPrefix(name, "-") {
			flag = flags.Lookup(strings.TrimPrefix(name, "-"))
		} else if len(name) == 1 {
			flag = flags.ShorthandLookup(name)
		} else {
			flag = flags.Lookup(name)
			if flag == nil {
				return nil, fmt.Errorf("unknown flag: -%s", name)
			}
			arg = "-" + arg
		}
		result = append(result, arg)
		if flag != nil && flag.NoOptDefVal == "" && !hasValue && i+1 < len(args) {
			i++
			result = append(result, args[i])
		}
	}
	return result, nil
}
