package sync_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/lablabs/luactl/internal/sync"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessModules(t *testing.T) {
	// Create the module directory with the required structure
	targetDir := filepath.Join(t.TempDir(), "tests", "fixture")
	require.NoError(t, os.MkdirAll(targetDir, 0755))

	// Read the expected output files
	expectedVariablesContent, err := os.ReadFile(filepath.Join("tests", "fixture", "variables-addon.tf"))
	require.NoError(t, err)

	// Configure a logger for testing
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create a test processor
	processor, err := sync.NewVariableProcessor(logger, "tests/fixture", targetDir, "modules")
	require.NoError(t, err)

	// Process the modules
	err = processor.ProcessModules(t.Context())
	require.NoError(t, err)

	actualVariablesPath := filepath.Join(targetDir, "variables-addon.tf")
	actualVariables, err := os.ReadFile(actualVariablesPath)
	require.NoError(t, err)

	// Compare the generated files with expected fixtures
	assert.Equal(t, string(expectedVariablesContent), string(actualVariables),
		"Generated variables-addon.tf doesn't match expected content")
}
