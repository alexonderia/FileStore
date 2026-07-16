package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexonderia/filestore/internal/client"
	"github.com/alexonderia/filestore/internal/config"
	"github.com/alexonderia/filestore/internal/domain"
)

const usage = `FileStore CLI

Usage:
  filestore help
  filestore version
  filestore config get [api-url|workspace]
  filestore config set <api-url|workspace> <value>
  filestore register --name NAME --email EMAIL --password-stdin
  filestore login --email EMAIL --password-stdin
  filestore logout
  filestore auth me
  filestore workspace show-base|list
  filestore workspace create <name>
  filestore workspace use <workspace-id>
  filestore workspace member add <workspace-id> <email> <owner|editor|viewer>
  filestore workspace member remove <workspace-id> <user-id>
  filestore upload [--workspace ID] [--name NAME] [--encoding NAME] <path>
  filestore file list [workspace-id]
  filestore file info <file-id>
  filestore file history <file-id>
  filestore file download [--version N] <file-id> <path>
  filestore file encoding <get|set> <file-id> [encoding]
  filestore file lock|lock-status|unlock <file-id>
  filestore file links <file-id>
  filestore update create --key KEY <file-id> <path>
  filestore update diff|resolve|reject <file-id> <session-id>
  filestore link revoke <file-id> <link-id>
  filestore link download <token> <path>
`

func Run(args []string, stdout, stderr io.Writer, getenv func(string) string, version string) int {
	return RunWithInput(args, os.Stdin, stdout, stderr, getenv, version)
}

func RunWithInput(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, version string) int {
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
	case "register", "login":
		if err := runCredentials(args[0], args[1:], stdin, stdout, getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		return 0
	case "logout":
		if err := runLogout(args[1:], stdout, getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		return 0
	case "auth":
		if err := runAuth(args[1:], stdout, getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		return 0
	case "workspace":
		if err := runWorkspace(args[1:], stdout, getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		return 0
	case "upload":
		if err := runUpload(args[1:], stdout, getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		return 0
	case "file":
		if err := runFile(args[1:], stdout, getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		return 0
	case "update":
		if err := runUpdate(args[1:], stdout, getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		return 0
	case "link":
		if err := runLink(args[1:], stdout, getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "error: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func runUpload(args []string, stdout io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("upload", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspaceID := flags.String("workspace", "", "workspace ID")
	name := flags.String("name", "", "logical file name")
	encoding := flags.String("encoding", "utf-8", "text encoding")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		return fmt.Errorf("usage: filestore upload [--workspace ID] [--name NAME] [--encoding NAME] <path>")
	}
	_, cfg, api, err := loadAPIClient(getenv)
	if err != nil {
		return err
	}
	if *workspaceID == "" {
		*workspaceID = cfg.WorkspaceID
	}
	if *workspaceID == "" {
		return fmt.Errorf("workspace is not selected; use --workspace or workspace use")
	}
	filePath := flags.Arg(0)
	source, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer source.Close()
	file, err := api.Upload(context.Background(), *workspaceID, *name, *encoding, filepath.Base(filePath), source)
	if err != nil {
		return err
	}
	return printJSON(stdout, file)
}

func runFile(args []string, stdout io.Writer, getenv func(string) string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: filestore file <list|info|history|download|encoding>")
	}
	_, cfg, api, err := loadAPIClient(getenv)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if len(args) > 2 {
			return fmt.Errorf("usage: filestore file list [workspace-id]")
		}
		workspaceID := cfg.WorkspaceID
		if len(args) == 2 {
			workspaceID = args[1]
		}
		if workspaceID == "" {
			return fmt.Errorf("workspace is not selected")
		}
		page, err := api.Files(context.Background(), workspaceID)
		if err != nil {
			return err
		}
		return printJSON(stdout, page)
	case "info":
		if len(args) != 2 {
			return fmt.Errorf("usage: filestore file info <file-id>")
		}
		file, err := api.File(context.Background(), args[1])
		if err != nil {
			return err
		}
		return printJSON(stdout, file)
	case "history":
		if len(args) != 2 {
			return fmt.Errorf("usage: filestore file history <file-id>")
		}
		page, err := api.History(context.Background(), args[1])
		if err != nil {
			return err
		}
		return printJSON(stdout, page)
	case "download":
		flags := flag.NewFlagSet("file download", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		version := flags.Int("version", 0, "version number")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 2 {
			return fmt.Errorf("usage: filestore file download [--version N] <file-id> <path>")
		}
		destination, err := os.Create(flags.Arg(1))
		if err != nil {
			return err
		}
		if err := api.Download(context.Background(), flags.Arg(0), *version, destination); err != nil {
			_ = destination.Close()
			return err
		}
		if err := destination.Close(); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, flags.Arg(1))
		return nil
	case "encoding":
		if len(args) == 3 && args[1] == "get" {
			file, err := api.File(context.Background(), args[2])
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(stdout, file.TextEncoding)
			return nil
		}
		if len(args) == 4 && args[1] == "set" {
			file, err := api.SetEncoding(context.Background(), args[2], args[3])
			if err != nil {
				return err
			}
			return printJSON(stdout, file)
		}
		return fmt.Errorf("usage: filestore file encoding <get FILE_ID|set FILE_ID ENCODING>")
	case "lock", "lock-status", "unlock":
		if len(args) != 2 {
			return fmt.Errorf("usage: filestore file %s <file-id>", args[0])
		}
		if args[0] == "unlock" {
			if err := api.Unlock(context.Background(), args[1]); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(stdout, "unlocked")
			return nil
		}
		var lock domain.FileLock
		if args[0] == "lock" {
			lock, err = api.Lock(context.Background(), args[1])
		} else {
			lock, err = api.LockStatus(context.Background(), args[1])
		}
		if err != nil {
			return err
		}
		return printJSON(stdout, lock)
	case "links":
		if len(args) != 2 {
			return fmt.Errorf("usage: filestore file links <file-id>")
		}
		page, err := api.Links(context.Background(), args[1])
		if err != nil {
			return err
		}
		return printJSON(stdout, page)
	default:
		return fmt.Errorf("unknown file command %q", args[0])
	}
}

func runUpdate(args []string, stdout io.Writer, getenv func(string) string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: filestore update <create|diff|resolve|reject>")
	}
	_, _, api, err := loadAPIClient(getenv)
	if err != nil {
		return err
	}
	if args[0] == "create" {
		flags := flag.NewFlagSet("update create", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		key := flags.String("key", "", "idempotency key")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 2 || len(*key) < 16 {
			return fmt.Errorf("usage: filestore update create --key KEY <file-id> <path> (KEY must be at least 16 characters)")
		}
		source, err := os.Open(flags.Arg(1))
		if err != nil {
			return err
		}
		defer source.Close()
		session, err := api.CreateUpdate(context.Background(), flags.Arg(0), *key, filepath.Base(flags.Arg(1)), source)
		if err != nil {
			return err
		}
		return printJSON(stdout, session)
	}
	if len(args) != 3 {
		return fmt.Errorf("usage: filestore update %s <file-id> <session-id>", args[0])
	}
	switch args[0] {
	case "diff":
		value, err := api.UpdateDiff(context.Background(), args[1], args[2])
		if err != nil {
			return err
		}
		return printJSON(stdout, value)
	case "resolve":
		value, err := api.ResolveUpdate(context.Background(), args[1], args[2])
		if err != nil {
			return err
		}
		return printJSON(stdout, value)
	case "reject":
		value, err := api.RejectUpdate(context.Background(), args[1], args[2])
		if err != nil {
			return err
		}
		return printJSON(stdout, value)
	default:
		return fmt.Errorf("unknown update command %q", args[0])
	}
}

func runLink(args []string, stdout io.Writer, getenv func(string) string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: filestore link <revoke|download>")
	}
	_, _, api, err := loadAPIClient(getenv)
	if err != nil {
		return err
	}
	switch args[0] {
	case "revoke":
		if len(args) != 3 {
			return fmt.Errorf("usage: filestore link revoke <file-id> <link-id>")
		}
		if err := api.RevokeLink(context.Background(), args[1], args[2]); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, "revoked")
		return nil
	case "download":
		if len(args) != 3 {
			return fmt.Errorf("usage: filestore link download <token> <path>")
		}
		destination, err := os.Create(args[2])
		if err != nil {
			return err
		}
		if err := api.DownloadLink(context.Background(), args[1], destination); err != nil {
			_ = destination.Close()
			return err
		}
		if err := destination.Close(); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, args[2])
		return nil
	default:
		return fmt.Errorf("unknown link command %q", args[0])
	}
}

func runCredentials(command string, args []string, stdin io.Reader, stdout io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("name", "", "display name")
	email := flags.String("email", "", "email")
	passwordStdin := flags.Bool("password-stdin", false, "read password from stdin")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !*passwordStdin || strings.TrimSpace(*email) == "" || (command == "register" && strings.TrimSpace(*name) == "") {
		return fmt.Errorf("usage: filestore %s %s--email EMAIL --password-stdin", command, map[bool]string{true: "--name NAME ", false: ""}[command == "register"])
	}
	password, err := readSecret(stdin)
	if err != nil {
		return err
	}
	path, cfg, api, err := loadAPIClient(getenv)
	if err != nil {
		return err
	}
	var result domain.AuthResult
	if command == "register" {
		result, err = api.Register(context.Background(), *name, *email, password)
	} else {
		result, err = api.Login(context.Background(), *email, password)
	}
	if err != nil {
		return err
	}
	cfg.Token = result.Token
	if err := config.SaveClient(path, cfg); err != nil {
		return err
	}
	return printJSON(stdout, result.User)
}

func runLogout(args []string, stdout io.Writer, getenv func(string) string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: filestore logout")
	}
	path, cfg, api, err := loadAPIClient(getenv)
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("not authenticated")
	}
	if err := api.Logout(context.Background()); err != nil {
		return err
	}
	cfg.Token = ""
	if err := config.SaveClient(path, cfg); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stdout, "logged out")
	return nil
}

func runAuth(args []string, stdout io.Writer, getenv func(string) string) error {
	if len(args) != 1 || args[0] != "me" {
		return fmt.Errorf("usage: filestore auth me")
	}
	_, _, api, err := loadAPIClient(getenv)
	if err != nil {
		return err
	}
	user, err := api.Me(context.Background())
	if err != nil {
		return err
	}
	return printJSON(stdout, user)
}

func runWorkspace(args []string, stdout io.Writer, getenv func(string) string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: filestore workspace <show-base|list|create|use|member>")
	}
	path, cfg, api, err := loadAPIClient(getenv)
	if err != nil {
		return err
	}
	switch args[0] {
	case "show-base":
		if len(args) != 1 {
			return fmt.Errorf("usage: filestore workspace show-base")
		}
		workspace, err := api.BaseWorkspace(context.Background())
		if err != nil {
			return err
		}
		return printJSON(stdout, workspace)
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: filestore workspace list")
		}
		workspaces, err := api.Workspaces(context.Background())
		if err != nil {
			return err
		}
		return printJSON(stdout, workspaces)
	case "create":
		if len(args) != 2 {
			return fmt.Errorf("usage: filestore workspace create <name>")
		}
		workspace, err := api.CreateWorkspace(context.Background(), args[1])
		if err != nil {
			return err
		}
		cfg.WorkspaceID = workspace.ID
		if err := config.SaveClient(path, cfg); err != nil {
			return err
		}
		return printJSON(stdout, workspace)
	case "use":
		if len(args) != 2 {
			return fmt.Errorf("usage: filestore workspace use <workspace-id>")
		}
		workspace, err := api.Workspace(context.Background(), args[1])
		if err != nil {
			return err
		}
		cfg.WorkspaceID = workspace.ID
		if err := config.SaveClient(path, cfg); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, workspace.ID)
		return nil
	case "member":
		return runWorkspaceMember(args[1:], stdout, api)
	default:
		return fmt.Errorf("unknown workspace command %q", args[0])
	}
}

func runWorkspaceMember(args []string, stdout io.Writer, api *client.Client) error {
	if len(args) == 4 && args[0] == "add" {
		role := domain.WorkspaceRole(args[3])
		member, err := api.PutMember(context.Background(), args[1], args[2], role)
		if err != nil {
			return err
		}
		return printJSON(stdout, member)
	}
	if len(args) == 3 && args[0] == "remove" {
		if err := api.RemoveMember(context.Background(), args[1], args[2]); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, "member removed")
		return nil
	}
	return fmt.Errorf("usage: filestore workspace member <add WORKSPACE EMAIL ROLE|remove WORKSPACE USER_ID>")
}

func loadAPIClient(getenv func(string) string) (string, config.Client, *client.Client, error) {
	path, err := config.ClientPath(getenv)
	if err != nil {
		return "", config.Client{}, nil, err
	}
	cfg, err := config.LoadClient(path)
	if err != nil {
		return "", config.Client{}, nil, err
	}
	return path, cfg, client.New(cfg.APIURL, cfg.Token), nil
}

func readSecret(reader io.Reader) (string, error) {
	value, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	value = strings.TrimSuffix(strings.TrimSuffix(value, "\n"), "\r")
	if value == "" {
		return "", fmt.Errorf("password must not be empty")
	}
	return value, nil
}

func printJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
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
