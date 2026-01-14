// Package sync handles synchronization of variables between modules.
package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// Configuration constants.
const (
	modulePrefix = "addon"

	sourceVariableFileName = "variables.tf"

	targetVariableFilePattern = "variables-%s.tf"
	targetFileMode            = 0600
)

// VariableProcessor handles the parsing, formatting, and writing of variables.
type VariableProcessor struct {
	tmpl       *template.Template
	logger     *slog.Logger
	workDir    string
	targetDir  string
	modulesDir string
}

// NewVariableProcessor creates a new processor.
func NewVariableProcessor(logger *slog.Logger, workDir, targetDir, modulesDir string) (*VariableProcessor, error) {
	tmpl, err := template.New("variables").Parse(
		`# IMPORTANT: This file is synced with the "terraform-aws-eks-universal-addon" template. Any changes to this file might be overwritten upon the next release of the template.
{{ printf "%s" .Variables }}`)
	if err != nil {
		logger.Error("Failed to create template", "error", err)
		return nil, fmt.Errorf("failed to create template: %w", err)
	}

	return &VariableProcessor{
		tmpl:       tmpl,
		logger:     logger,
		workDir:    workDir,
		targetDir:  targetDir,
		modulesDir: filepath.Join(workDir, modulesDir),
	}, nil
}

// ProcessModules finds and processes all relevant addon modules in the specified directory.
func (vp *VariableProcessor) ProcessModules(ctx context.Context) error {
	vp.logger.InfoContext(ctx, "Starting variable sync from modules", "modulesDir", vp.modulesDir)

	entries, err := os.ReadDir(vp.modulesDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			vp.logger.WarnContext(ctx, "Modules directory not found, skipping sync", "modulesDir", vp.modulesDir)
			return nil
		}
		vp.logger.ErrorContext(ctx, "Failed to read directory", "path", vp.modulesDir, "error", err)
		return fmt.Errorf("failed to read directory %q: %w", vp.modulesDir, err)
	}

	var (
		processedCount = 0
		skippedCount   = 0
		errorCount     = 0
	)

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			vp.logger.WarnContext(ctx, "Processing cancelled", "error", ctx.Err())
			return ctx.Err()
		default:
		}

		if !entry.IsDir() {
			continue
		}

		moduleName := entry.Name()
		if !strings.HasPrefix(moduleName, modulePrefix) || strings.Contains(moduleName, ".") {
			vp.logger.DebugContext(ctx, "Skipping entry", "module", moduleName, "reason", "does not match criteria")
			skippedCount++
			continue
		}

		vp.logger.InfoContext(ctx, "Processing module", "module", moduleName)
		procErr := vp.processModule(ctx, moduleName)
		if procErr != nil {
			errorCount++
			vp.logger.ErrorContext(ctx, "Failed to process module", "module", moduleName, "error", procErr)
		} else {
			processedCount++
		}
	}

	vp.logger.InfoContext(ctx, "Variable sync finished",
		"processed", processedCount,
		"skipped", skippedCount,
		"errors", errorCount,
	)
	if errorCount > 0 {
		return fmt.Errorf("encountered %d error(s) during processing", errorCount)
	}

	return nil
}

func (vp *VariableProcessor) processModule(ctx context.Context, moduleName string) error {
	sourcePath := filepath.Join(vp.modulesDir, moduleName, "modules", moduleName, sourceVariableFileName)

	vp.logger.DebugContext(ctx, "Processing source file", "sourcePath", sourcePath)

	file, err := vp.extractVariables(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("failed to parse variables: %w", err)
	}

	syncVarErr := vp.syncAddonVariables(ctx, moduleName, file)
	if syncVarErr != nil {
		return fmt.Errorf("failed to sync addon variables: %w", syncVarErr)
	}

	return nil
}

func (vp *VariableProcessor) extractVariables(ctx context.Context, filePath string) (*hclwrite.File, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		vp.logger.ErrorContext(ctx, "Failed to read file", "path", filePath, "error", err)
		return nil, fmt.Errorf("failed to read file %q: %w", filePath, err)
	}

	file, diags := hclwrite.ParseConfig(src, filepath.Base(filePath), hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		diagErr := errors.New(diags.Error())
		vp.logger.ErrorContext(ctx, "Failed to parse HCL", "path", filePath, "error", diagErr)
		return nil, fmt.Errorf("failed to parse HCL file %q: %w", filePath, diagErr)
	}

	varFile := hclwrite.NewEmptyFile()
	for _, block := range file.Body().Blocks() {
		if block.Type() != "variable" {
			continue
		}

		labels := block.Labels()
		if len(labels) == 0 {
			continue
		}

		varFile.Body().AppendBlock(block)
	}

	return varFile, nil
}

func (vp *VariableProcessor) syncAddonVariables(ctx context.Context, moduleName string, varFile *hclwrite.File) error {
	file := hclwrite.NewEmptyFile()
	for _, block := range varFile.Body().Blocks() {
		labels := block.Labels()

		// Skip the "enabled" variable
		if labels[0] == "enabled" {
			continue
		}

		variable := hclwrite.NewBlock("variable", labels)
		variable.Body().SetAttributeRaw("type", block.Body().GetAttribute("type").Expr().BuildTokens(nil))
		variable.Body().SetAttributeRaw("default", hclwrite.TokensForValue(cty.NullVal(cty.String)))

		// Update descriptionAtrribute with default value if both exist
		if descriptionAtrribute := block.Body().GetAttribute("description"); descriptionAtrribute != nil {
			descriptionValue := string(descriptionAtrribute.Expr().BuildTokens(nil).Bytes())
			descriptionValue = strings.TrimSpace(descriptionValue)
			descriptionValue = strings.Trim(descriptionValue, "\"")
			descriptionValue = strings.ReplaceAll(descriptionValue, "\"", "")

			if defaultAttribute := block.Body().GetAttribute("default"); defaultAttribute != nil {
				defaultValue := string(defaultAttribute.Expr().BuildTokens(nil).Bytes())
				defaultValue = strings.TrimSpace(defaultValue)

				typeAttribute := string(block.Body().GetAttribute("type").Expr().BuildTokens(nil).Bytes())
				typeAttribute = strings.TrimSpace(typeAttribute)
				typeAttribute = strings.Trim(typeAttribute, "\"")
				if typeAttribute == "string" {
					defaultValue = strings.Trim(defaultValue, "\"")
				}

				if defaultValue == "" {
					defaultValue = "\"\""
				}

				descriptionValue = fmt.Sprintf("%s Defaults to `%s`.", descriptionValue, defaultValue)
			}

			variable.Body().SetAttributeRaw("description", hclwrite.TokensForValue(cty.StringVal(descriptionValue)))
		}

		file.Body().AppendNewline()
		file.Body().AppendBlock(variable)
	}

	var buf bytes.Buffer
	err := vp.tmpl.Execute(&buf, &struct {
		Variables []byte
	}{
		Variables: file.Bytes(),
	})
	if err != nil {
		vp.logger.ErrorContext(ctx, "Failed to execute template", "moduleName", moduleName, "error", err)
		return fmt.Errorf("failed to execute template for module %q: %w", moduleName, err)
	}

	targetPath := filepath.Join(vp.targetDir, fmt.Sprintf(targetVariableFilePattern, moduleName))
	err = os.WriteFile(targetPath, buf.Bytes(), targetFileMode)
	if err != nil {
		vp.logger.ErrorContext(ctx, "Failed to write file", "path", targetPath, "error", err)
		return fmt.Errorf("failed to write file %q: %w", targetPath, err)
	}

	vp.logger.InfoContext(ctx, "Successfully wrote addon variables", "targetPath", targetPath)
	return nil
}
