// Package cmd provides command-line interfaces for luactl.
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/lablabs/luactl/internal/sync"
)

// NewSyncCmd creates and returns the sync command.
func NewSyncCmd() *cobra.Command {
	var workDir, targetDir, modulesDir string

	// syncCmd represents the sync command.
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Syncs variables from addon submodules to the root module",
		Long: `Reads variables.tf files from nested addon modules within the
.terraform/modules directory and generates corresponding variables-<addon-name>.tf
files in the current directory.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, cancel := CreateContextWithTimeout()
			defer cancel()

			logger := GetLogger()

			processor, err := sync.NewVariableProcessor(logger, workDir, targetDir, modulesDir)
			if err != nil {
				logger.Error("Failed to initialize variable processor", "error", err)
				return err
			}

			syncErr := processor.ProcessModules(ctx)
			if syncErr != nil {
				logger.Error("Variable synchronization failed", "error", syncErr)
				return syncErr
			}

			logger.Info("Variable synchronization completed successfully")
			return nil
		},
	}

	syncCmd.Flags().StringVarP(&workDir, "work-dir", "", ".",
		"Directory to work in")
	syncCmd.Flags().StringVarP(&targetDir, "target-dir", "", ".",
		"Directory to output synced files to")
	syncCmd.Flags().StringVarP(&modulesDir, "modules-dir", "", ".terraform/modules",
		"Directory containing Terraform modules, will be prepended by work-dir")

	return syncCmd
}
