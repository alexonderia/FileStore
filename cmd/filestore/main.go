package main

import (
	"os"

	"github.com/alexonderia/filestore/internal/cli"
	"github.com/alexonderia/filestore/internal/config"
)

var version = "dev"

func main() {
	if err := config.LoadDotEnv(".env"); err != nil {
		_, _ = os.Stderr.WriteString("failed to load .env: " + err.Error() + "\n")
		os.Exit(2)
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv, version))
}
