package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/alexonderia/filestore/internal/config"
)

const usage = `FileStore CLI

Usage:
  filestore help
  filestore version
  filestore config get [api-url|workspace]
  filestore config set <api-url|workspace> <value>
`

func Run(args []string, stdout, stderr io.Writer, getenv func(string) string, version string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	}

	switch args[0] {
	case "version", "--version":
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	case "config":
		if err := runConfig(args[1:], stdout, getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "error: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func runConfig(args []string, stdout io.Writer, getenv func(string) string) error {
	path, err := config.ClientPath(getenv)
	if err != nil {
		return err
	}
	cfg, err := config.LoadClient(path)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: filestore config <get|set>")
	}

	switch args[0] {
	case "get":
		if len(args) > 2 {
			return fmt.Errorf("usage: filestore config get [api-url|workspace]")
		}
		key := ""
		if len(args) == 2 {
			key = args[1]
		}
		return printConfig(stdout, cfg, key)
	case "set":
		if len(args) != 3 {
			return fmt.Errorf("usage: filestore config set <api-url|workspace> <value>")
		}
		if err := setConfig(&cfg, args[1], args[2]); err != nil {
			return err
		}
		if err := config.SaveClient(path, cfg); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "%s updated\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func printConfig(output io.Writer, cfg config.Client, key string) error {
	switch key {
	case "":
		_, _ = fmt.Fprintf(output, "api-url=%s\nworkspace=%s\n", cfg.APIURL, cfg.WorkspaceID)
	case "api-url":
		_, _ = fmt.Fprintln(output, cfg.APIURL)
	case "workspace":
		_, _ = fmt.Fprintln(output, cfg.WorkspaceID)
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}

func setConfig(cfg *config.Client, key, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case "api-url":
		cfg.APIURL = value
	case "workspace":
		cfg.WorkspaceID = value
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return cfg.Validate()
}
