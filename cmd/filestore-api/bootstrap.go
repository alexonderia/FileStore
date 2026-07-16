package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/alexonderia/filestore/internal/auth"
	"github.com/alexonderia/filestore/internal/database"
	"github.com/alexonderia/filestore/internal/repository/postgres"
	"github.com/alexonderia/filestore/internal/service"
)

func runBootstrapSuperadmin(args []string, logger *slog.Logger) int {
	flags := flag.NewFlagSet("bootstrap-superadmin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	databaseURL := flags.String("database-url", os.Getenv("FILESTORE_DATABASE_URL"), "PostgreSQL connection URL")
	name := flags.String("name", "", "superadmin display name")
	email := flags.String("email", "", "superadmin email")
	passwordStdin := flags.Bool("password-stdin", false, "read password from stdin")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !*passwordStdin {
		logger.Error("invalid bootstrap arguments", "usage", "filestore-api bootstrap-superadmin --name NAME --email EMAIL --password-stdin")
		return 2
	}
	password, err := readPassword(os.Stdin)
	if err != nil {
		logger.Error("read bootstrap password", "error", err)
		return 2
	}
	ctx := context.Background()
	pool, err := database.Open(ctx, *databaseURL)
	if err != nil {
		logger.Error("database startup failed", "error", err)
		return 1
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		logger.Error("database migration failed", "error", err)
		return 1
	}
	userID, err := service.NewBootstrap(postgres.NewUsers(pool), auth.DefaultPasswordHasher()).Superadmin(ctx, *name, *email, password)
	if err != nil {
		logger.Error("bootstrap superadmin failed", "error", err)
		return 1
	}
	logger.Info("superadmin is ready", "user_id", userID, "email", strings.ToLower(strings.TrimSpace(*email)))
	return 0
}

func readPassword(reader io.Reader) (string, error) {
	password, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	password = strings.TrimSuffix(strings.TrimSuffix(password, "\n"), "\r")
	if password == "" {
		return "", fmt.Errorf("password must not be empty")
	}
	return password, nil
}
