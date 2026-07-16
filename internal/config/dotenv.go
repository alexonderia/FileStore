package config

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

// LoadDotEnv loads KEY=VALUE pairs from path into the process environment.
// Existing environment variables keep their current values.
func LoadDotEnv(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return errors.New("dotenv: expected KEY=VALUE line")
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("dotenv: variable name must not be empty")
		}
		if _, exists := os.LookupEnv(name); exists {
			continue
		}
		if err := os.Setenv(name, strings.TrimSpace(value)); err != nil {
			return err
		}
	}
	return scanner.Err()
}
