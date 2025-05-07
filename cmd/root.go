// Package cmd provides command-line interfaces for luactl.
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// Default timeout duration for commands.
const defaultCommandTimeout = 2 * time.Minute

// appConfig holds application-level configurations to avoid global variables.
type appConfig struct {
	logLevel string
	timeout  time.Duration
	logger   *slog.Logger
	rootCmd  *cobra.Command
}

// newAppConfig initializes and returns a new application configuration.
func newAppConfig() *appConfig {
	config := &appConfig{
		logLevel: "info",
		timeout:  defaultCommandTimeout,
	}

	// rootCmd represents the base command when called without any subcommands.
	config.rootCmd = &cobra.Command{
		Use:   "luactl", // Choose a name for your CLI tool
		Short: "A tool for managing LARA universal-addon",
		Long: `luactl provides utilities for working with
terraform-aws-eks-universal-addon, such as syncing variables.`,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			// Configure slog based on the persistent flag
			var level slog.Level
			switch config.logLevel {
			case "debug":
				level = slog.LevelDebug
			case "info":
				level = slog.LevelInfo
			case "warn", "warning":
				level = slog.LevelWarn
			case "error":
				level = slog.LevelError
			default:
				return fmt.Errorf("invalid log level: %s", config.logLevel)
			}

			opts := &slog.HandlerOptions{
				Level: level,
			}
			handler := slog.NewTextHandler(os.Stderr, opts)
			config.logger = slog.New(handler)
			slog.SetDefault(config.logger)

			config.logger.Debug("Using log level", "level", level.String())
			return nil
		},
	}

	return config
}

// Global app configuration instance.
var cfg = struct { //nolint:gochecknoglobals // Use of global variable simplifies Cobra command structure
	app *appConfig
}{
	app: newAppConfig(),
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	// Create context with timeout for all commands
	// Note: Cobra doesn't directly support passing context down easily like this
	// in PersistentPreRun. We'll handle context within each command's RunE.
	// However, setting up the logger here is fine.

	err := cfg.app.rootCmd.Execute()
	if err != nil {
		// Cobra prints the error, so we just exit
		os.Exit(1)
	}
}

// AddCommand adds a command to the root command.
func AddCommand(cmd *cobra.Command) {
	cfg.app.rootCmd.AddCommand(cmd)
}

// Setup initializes flags and commands.
func Setup() {
	// Add persistent flags available to all commands
	cfg.app.rootCmd.PersistentFlags().StringVarP(&cfg.app.logLevel, "log-level", "l", "info",
		"Set logging level (debug, info, warn, error)")
	cfg.app.rootCmd.PersistentFlags().DurationVarP(&cfg.app.timeout, "timeout", "t", defaultCommandTimeout,
		"Global command timeout")
}

// GetLogger returns the configured logger.
func GetLogger() *slog.Logger {
	return cfg.app.logger
}

// GetTimeout returns the configured timeout duration.
func GetTimeout() time.Duration {
	return cfg.app.timeout
}

// CreateContextWithTimeout creates a context with the configured timeout.
func CreateContextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), cfg.app.timeout)
}
