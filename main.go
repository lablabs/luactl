// Package main is the entry point for luactl.
package main

import (
	"github.com/lablabs/luactl/cmd"
)

func main() {
	// Set up the root command and add subcommands
	cmd.Setup()
	cmd.AddCommand(cmd.NewSyncCmd())

	// Execute the root command
	cmd.Execute()
}
