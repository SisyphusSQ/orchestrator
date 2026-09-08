// Package cmd 定义纯 HTTP 管理命令，不初始化服务端运行时。
package cmd

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/openark/orchestrator/tools/orch-cli/internal/client"
	"github.com/spf13/cobra"
)

//go:embed catalog.json
var catalogJSON []byte

type specification struct {
	Name, Summary, Group, Path, Method, Projection string
	Query                                          map[string]string
	Required                                       []string
	ReadOnly, Local, Body                          bool
}
type usageError struct{ error }
type partialError struct{ error }

// ExitCode 返回稳定的进程状态：1 执行失败，2 参数，3 结果未知，4 部分失败。
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := errors.AsType[*client.Error](err); ok && e.Unknown {
		return 3
	}
	if _, ok := errors.AsType[*partialError](err); ok {
		return 4
	}
	if _, ok := errors.AsType[*usageError](err); ok {
		return 2
	}
	return 1
}

var placeholder = regexp.MustCompile(`\{([a-z-]+)(\?)?\}`)

func catalog() []specification {
	var specs []specification
	if err := json.Unmarshal(catalogJSON, &specs); err != nil {
		panic(err)
	}
	return specs
}

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer, version, commit string) error {
	var config client.Config
	var output string
	root := &cobra.Command{Use: "orch", Short: "Manage orchestrator through HTTP", SilenceUsage: true, SilenceErrors: true, Version: version + "\n" + commit, Args: cobra.NoArgs}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetArgs(args)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return &usageError{err} })
	flags := root.PersistentFlags()
	flags.StringSliceVar(&config.Endpoints, "endpoint", strings.FieldsFunc(cmp.Or(os.Getenv("ORCH_ENDPOINT"), "http://localhost:3000"), func(r rune) bool { return r == ',' || r == ' ' }), "HTTP API endpoints (comma separated or repeated)")
	flags.StringVar(&config.Username, "user", os.Getenv("ORCH_USER"), "Basic authentication user")
	flags.StringVar(&config.Password, "password", os.Getenv("ORCH_PASSWORD"), "Basic authentication password; prefer ORCH_PASSWORD")
	flags.StringVar(&config.Token, "token", os.Getenv("ORCH_TOKEN"), "Access token public:secret; prefer ORCH_TOKEN")
	flags.StringArrayVar(&config.Headers, "header", nil, "Authentication header (repeatable)")
	flags.StringVar(&config.CA, "ca", os.Getenv("ORCH_CA"), "Trusted CA PEM file")
	flags.StringVar(&config.Cert, "cert", os.Getenv("ORCH_CERT"), "Client certificate PEM file")
	flags.StringVar(&config.Key, "key", os.Getenv("ORCH_KEY"), "Client private key file")
	flags.DurationVar(&config.Timeout, "timeout", 30*time.Second, "Per-request timeout")
	flags.StringVar(&output, "output", "text", "Output format: text or json")
	root.PersistentPreRunE = func(command *cobra.Command, _ []string) error {
		if command.Name() == "help" || command.Name() == "completion" || (command.Parent() != nil && command.Parent().Name() == "completion") {
			return nil
		}
		if output != "text" && output != "json" {
			return &usageError{fmt.Errorf("output must be text or json")}
		}
		if !flags.Changed("timeout") && os.Getenv("ORCH_TIMEOUT") != "" {
			v, err := time.ParseDuration(os.Getenv("ORCH_TIMEOUT"))
			if err != nil {
				return &usageError{fmt.Errorf("invalid ORCH_TIMEOUT")}
			}
			config.Timeout = v
		}
		return nil
	}
	root.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	groups := map[string]bool{}
	for _, spec := range catalog() {
		values := map[string]*string{}
		cmd := &cobra.Command{Use: spec.Name, Short: spec.Summary, Args: func(_ *cobra.Command, a []string) error {
			if len(a) > 0 {
				return &usageError{fmt.Errorf("unexpected positional arguments")}
			}
			return nil
		}, GroupID: spec.Group}
		if !groups[spec.Group] {
			root.AddGroup(&cobra.Group{ID: spec.Group, Title: spec.Group + ":"})
			groups[spec.Group] = true
		}
		add := func(name string) {
			if values[name] != nil {
				return
			}
			short := ""
			if name == "instance" {
				short = "i"
			}
			if name == "destination" {
				short = "d"
			}
			def := ""
			if name == "include-downtimed" {
				def = "false"
			}
			if name == "promotion-rule" {
				def = "prefer"
			}
			values[name] = cmd.Flags().StringP(name, short, def, name)
			if name == "strict" || name == "include-downtimed" {
				cmd.Flags().Lookup(name).NoOptDefVal = "true"
			}
		}
		for _, m := range placeholder.FindAllStringSubmatch(spec.Path, -1) {
			add(m[1])
			if m[1] == "cluster" {
				add("instance")
				add("alias")
			}
		}
		for name := range spec.Query {
			add(name)
		}
		if spec.Body {
			add("body")
		}
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			if spec.Name == "submit-pool-instances" && !cmd.Flags().Changed("instances") {
				return &usageError{fmt.Errorf("--instances is required; use an explicit empty value to clear the pool")}
			}
			v := map[string]string{}
			for k, p := range values {
				v[k] = *p
			}
			requests, err := prepare(spec, v)
			if err != nil {
				return &usageError{err}
			}
			c, err := client.New(config)
			if err != nil {
				return &usageError{err}
			}
			defer c.Close()
			results := []json.RawMessage{}
			for _, request := range requests {
				body, err := c.Do(cmd.Context(), request)
				var result json.RawMessage
				if err == nil {
					result, err = project(body, spec.Projection)
					if err != nil {
						err = &client.Error{Kind: "response", Message: "unexpected command response", Unknown: request.Mutating}
					}
				}
				if err != nil {
					if len(results) > 0 {
						if output == "json" {
							if outputErr := json.NewEncoder(stdout).Encode(results); outputErr != nil {
								return &partialError{errors.Join(err, outputErr)}
							}
						}
						return &partialError{fmt.Errorf("%d of %d operations succeeded; batch stopped: %w", len(results), len(requests), err)}
					}
					return err
				}
				results = append(results, result)
				if output == "text" {
					if err := render(stdout, result); err != nil {
						return err
					}
				}
			}
			if output == "json" {
				if len(results) == 1 {
					return json.NewEncoder(stdout).Encode(results[0])
				}
				return json.NewEncoder(stdout).Encode(results)
			}
			return nil
		}
		root.AddCommand(cmd)
	}
	root.AddGroup(&cobra.Group{ID: "client", Title: "Client:"})
	root.SetHelpCommandGroupID("client")
	root.SetCompletionCommandGroupID("client")
	root.AddCommand(&cobra.Command{Use: "which-api", Short: "Print the selected API endpoint", Args: cobra.NoArgs, GroupID: "client", RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := client.New(config)
		if err != nil {
			return &usageError{err}
		}
		defer c.Close()
		u, err := c.Endpoint(cmd.Context(), false)
		if err != nil {
			return err
		}
		if output == "json" {
			return json.NewEncoder(stdout).Encode(u.String())
		}
		_, err = fmt.Fprintln(stdout, u.String())
		return err
	}})
	var method, body string
	api := &cobra.Command{Use: "api PATH", Short: "Call a relative API path; never retries unknown operations", Args: cobra.ExactArgs(1), GroupID: "client", RunE: func(cmd *cobra.Command, a []string) error {
		u, err := url.Parse(a[0])
		if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" {
			return &usageError{fmt.Errorf("invalid relative API path")}
		}
		request := client.Request{Method: strings.ToUpper(method), Path: u.EscapedPath(), Query: u.Query(), Body: json.RawMessage(body), Mutating: true, Local: true}
		if err := client.Validate(request); err != nil {
			return &usageError{err}
		}
		c, err := client.New(config)
		if err != nil {
			return &usageError{err}
		}
		defer c.Close()
		result, err := c.Do(cmd.Context(), request)
		if err != nil {
			return err
		}
		if output == "json" {
			return json.NewEncoder(stdout).Encode(result)
		}
		return render(stdout, result)
	}}
	api.Flags().StringVar(&method, "method", "GET", "HTTP method")
	api.Flags().StringVar(&body, "body", "", "JSON request body")
	root.AddCommand(api)
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	err := root.ExecuteContext(ctx)
	if err != nil && (strings.HasPrefix(err.Error(), "unknown command") || strings.HasPrefix(err.Error(), "accepts ") || strings.HasPrefix(err.Error(), "requires ")) {
		return &usageError{err}
	}
	return err
}

func prepare(spec specification, values map[string]string) ([]client.Request, error) {
	values = maps.Clone(values)
	if strings.Contains(spec.Path, "{cluster") {
		if values["cluster"] != "" && (values["alias"] != "" || values["instance"] != "") {
			return nil, fmt.Errorf("cluster cannot be combined with alias or instance")
		}
		if values["alias"] != "" && values["instance"] != "" {
			return nil, fmt.Errorf("alias cannot be combined with instance")
		}
		values["cluster"] = cmp.Or(values["cluster"], values["alias"], values["instance"])
	}
	for _, required := range spec.Required {
		if values[required] == "" {
			return nil, fmt.Errorf("--%s is required", required)
		}
	}
	if rule := values["promotion-rule"]; rule != "" && !slices.Contains([]string{"prefer", "neutral", "prefer_not", "must_not"}, rule) {
		return nil, fmt.Errorf("invalid promotion rule")
	}
	if seconds := values["seconds"]; seconds != "" {
		n, err := strconv.ParseUint(seconds, 10, 32)
		if err != nil || n > 2147483647 {
			return nil, fmt.Errorf("seconds must be a nonnegative 32-bit integer")
		}
	}
	for _, name := range []string{"strict", "include-downtimed"} {
		if value := values[name]; value != "" && value != "true" && value != "false" {
			return nil, fmt.Errorf("--%s must be true or false", name)
		}
	}
	if duration := values["duration"]; duration != "" {
		units := map[byte]uint64{'s': 1, 'm': 60, 'h': 3600, 'd': 86400, 'w': 604800}
		multiplier := units[duration[len(duration)-1]]
		n, err := strconv.ParseUint(duration[:len(duration)-1], 10, 64)
		if err != nil || multiplier == 0 || n > 2147483647/multiplier {
			return nil, fmt.Errorf("duration must be a nonnegative integer with s/m/h/d/w suffix, at most 2147483647 seconds")
		}
	}
	if raw := values["binlog"]; raw != "" && (spec.Name == "master-pos-wait" || strings.HasPrefix(spec.Name, "correlate-")) {
		file, pos, ok := strings.Cut(raw, ":")
		_, err := strconv.ParseUint(pos, 10, 63)
		if !ok || file == "" || err != nil {
			return nil, fmt.Errorf("binlog must be file:position with a nonnegative position")
		}
	}
	if values["destination"] != "" && spec.Query["destination"] != "" {
		host, port, err := hostport(values["destination"])
		if err != nil {
			return nil, err
		}
		values["destination"] = net.JoinHostPort(host, port)
	}
	if raw := values["pattern"]; raw != "" && spec.Name != "find-binlog-entry" {
		if _, err := regexp.Compile(raw); err != nil {
			return nil, fmt.Errorf("invalid pattern: %w", err)
		}
	}
	if spec.Name == "submit-pool-instances" {
		keys := []string{}
		for _, raw := range strings.FieldsFunc(values["instances"], func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
			host, port, err := hostport(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid pool instance: %w", err)
			}
			keys = append(keys, net.JoinHostPort(host, port))
		}
		values["instances"] = strings.Join(keys, ",")
	}
	instances := []string{values["instance"]}
	if strings.Contains(spec.Path, "{instance}") {
		instances = strings.FieldsFunc(values["instance"], func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
		if len(instances) == 0 {
			return nil, fmt.Errorf("--instance is required")
		}
	}
	requests := []client.Request{}
	for _, instance := range instances {
		values["instance"] = instance
		var pathErr error
		path := placeholder.ReplaceAllStringFunc(spec.Path, func(token string) string {
			m := placeholder.FindStringSubmatch(token)
			name := m[1]
			v := values[name]
			if v == "" {
				if m[2] != "?" {
					pathErr = fmt.Errorf("--%s is required", name)
				}
				return ""
			}
			if name == "instance" || name == "destination" {
				host, port, err := hostport(v)
				if err != nil {
					pathErr = fmt.Errorf("invalid --%s: %w", name, err)
					return ""
				}
				return url.PathEscape(host) + "/" + port
			}
			return url.PathEscape(v)
		})
		if pathErr != nil {
			return nil, pathErr
		}
		path = strings.TrimRight(path, "/")
		query := url.Values{}
		for name, key := range spec.Query {
			if v := values[name]; v != "" {
				query.Set(key, v)
			}
		}
		r := client.Request{Method: cmp.Or(spec.Method, "GET"), Path: path, Query: query, Mutating: !spec.ReadOnly, Local: spec.Local}
		if spec.Body {
			r.Body = json.RawMessage(cmp.Or(values["body"], "{}"))
		}
		if err := client.Validate(r); err != nil {
			return nil, err
		}
		requests = append(requests, r)
	}
	return requests, nil
}

func hostport(value string) (string, string, error) {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		if strings.Contains(value, ":") {
			return "", "", fmt.Errorf("expected host:port (IPv6 requires brackets)")
		}
		host = value
		port = "3306"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || host == "" || strings.ContainsAny(host, " /\\\r\n\t") {
		return "", "", fmt.Errorf("invalid host or port")
	}
	return host, strconv.Itoa(n), nil
}
func project(body json.RawMessage, projection string) (json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) == nil && envelope["Code"] != nil {
		if details := envelope["Details"]; len(details) > 0 && !bytes.Equal(details, []byte("null")) {
			body = details
		} else {
			body = envelope["Message"]
		}
	}
	if projection == "" {
		return body, nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	switch projection {
	case "leader-hostname":
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected leader response")
		}
		address, _ := obj["address"].(string)
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("leader address unavailable")
		}
		value = host
	case "replicating", "stopped":
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected instance response")
		}
		state := "1"
		if projection == "stopped" {
			state = "0"
		}
		value = fmt.Sprint(obj["ReplicationSQLThreadState"]) == state && fmt.Sprint(obj["ReplicationIOThreadState"]) == state
	case "broken-replicas", "running-replicas":
		items, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("expected replica list")
		}
		filtered := []any{}
		for _, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			running := fmt.Sprint(obj["ReplicationSQLThreadState"]) == "1" && fmt.Sprint(obj["ReplicationIOThreadState"]) == "1"
			broken := !running && (obj["LastSQLError"] != "" || obj["LastIOError"] != "")
			if projection == "running-replicas" && running || projection == "broken-replicas" && broken {
				filtered = append(filtered, item)
			}
		}
		value = filtered
	case "dominant-dc":
		items, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("expected master list")
		}
		counts := map[string]int{}
		best := ""
		for _, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			dc, _ := obj["DataCenter"].(string)
			counts[dc]++
			if counts[dc] > counts[best] || counts[dc] == counts[best] && dc < best {
				best = dc
			}
		}
		value = best
	case "cluster-aliases": // 保留完整集群对象供 JSON 消费，文本由统一渲染器处理。
	default:
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected object response for %s", projection)
		}
		var exists bool
		value, exists = obj[projection]
		if !exists {
			return nil, fmt.Errorf("response missing %s", projection)
		}
	}
	return json.Marshal(value)
}
func render(w io.Writer, raw json.RawMessage) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	return renderValue(w, value)
}
func renderValue(w io.Writer, value any) error {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil
		}
		_, err := fmt.Fprintln(w, v)
		return err
	case []any:
		for _, item := range v {
			if err := renderValue(w, item); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		if analysis, ok := v["Analysis"].(string); ok {
			parts := []string{}
			if analysis != "NoProblem" {
				parts = append(parts, analysis)
			}
			if warnings, ok := v["StructureAnalysis"].([]any); ok {
				for _, warning := range warnings {
					parts = append(parts, fmt.Sprint(warning))
				}
			}
			analysis = strings.Join(parts, ", ")
			key, kok := v["AnalyzedInstanceKey"].(map[string]any)
			cluster, cok := v["ClusterDetails"].(map[string]any)
			if kok && cok {
				_, err := fmt.Fprintf(w, "%s (cluster %v): %s\n", net.JoinHostPort(fmt.Sprint(key["Hostname"]), fmt.Sprint(key["Port"])), cluster["ClusterName"], analysis)
				return err
			}
		}
		if name, ok := v["ClusterName"].(string); ok {
			if alias, ok := v["ClusterAlias"].(string); ok {
				_, err := fmt.Fprintf(w, "%s %s\n", name, alias)
				return err
			}
		}
		if key, ok := v["Key"].(map[string]any); ok {
			return renderValue(w, key)
		}
		if host, ok := v["Hostname"].(string); ok && v["Port"] != nil {
			_, err := fmt.Fprintln(w, net.JoinHostPort(host, fmt.Sprint(v["Port"])))
			return err
		}
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
