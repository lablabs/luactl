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
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// Configuration constants.
const (
	addonPrefix               = "addon"
	sourceVariableFileName    = "variables.tf"
	targetAddonFilePattern    = "%s.tf"
	targetVariableFilePattern = "variables-%s.tf"
	targetFileMode            = 0600
)

// VariableProcessor handles the parsing, formatting, and writing of variables.
type VariableProcessor struct {
	tmpl   *template.Template
	logger *slog.Logger
}

// NewVariableProcessor creates a new processor.
func NewVariableProcessor(logger *slog.Logger) (*VariableProcessor, error) {
	tmpl, err := template.New("variables").Parse(
		`# IMPORTANT: This file is synced with the "terraform-aws-eks-universal-addon" module. Any changes to this file might be overwritten upon the next release of that module.
{{ printf "%s" .Variables }}`)
	if err != nil {
		logger.Error("Failed to create template", "error", err)
		return nil, fmt.Errorf("failed to create template: %w", err)
	}

	return &VariableProcessor{
		tmpl:   tmpl,
		logger: logger,
	}, nil
}

// ProcessModules finds and processes all relevant addon modules in the specified directory.
func (vp *VariableProcessor) ProcessModules(ctx context.Context, modulesDir string) error {
	vp.logger.InfoContext(ctx, "Starting variable sync from modules", "modulesDir", modulesDir)

	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			vp.logger.WarnContext(ctx, "Modules directory not found, skipping sync", "modulesDir", modulesDir)
			return nil
		}
		vp.logger.ErrorContext(ctx, "Failed to read directory", "path", modulesDir, "error", err)
		return fmt.Errorf("failed to read directory %q: %w", modulesDir, err)
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
		if !strings.HasPrefix(moduleName, addonPrefix) || strings.Contains(moduleName, ".") {
			vp.logger.DebugContext(ctx, "Skipping entry", "module", moduleName, "reason", "does not match criteria")
			skippedCount++
			continue
		}

		vp.logger.InfoContext(ctx, "Processing module", "module", moduleName)
		procErr := vp.processSingleModule(ctx, modulesDir, moduleName)
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

func (vp *VariableProcessor) processSingleModule(ctx context.Context, modulesBaseDir, moduleName string) error {
	sourcePath := filepath.Join(modulesBaseDir, moduleName, "modules", moduleName, sourceVariableFileName)

	vp.logger.DebugContext(ctx, "Processing source file", "sourcePath", sourcePath)

	file, err := vp.extractVariables(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("failed to parse variables: %w", err)
	}

	syncErr := vp.syncAddonDefaults(ctx, moduleName, file)
	if syncErr != nil {
		return fmt.Errorf("failed to sync addon defaults: %w", syncErr)
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

func (vp *VariableProcessor) syncAddonDefaults(ctx context.Context, moduleName string, varFile *hclwrite.File) error {
	filePath := fmt.Sprintf(targetAddonFilePattern, moduleName)

	src, err := os.ReadFile(filePath)
	if err != nil {
		vp.logger.ErrorContext(ctx, "Failed to read file", "path", filePath, "error", err)
		return fmt.Errorf("failed to read file %q: %w", filePath, err)
	}

	file, diags := hclwrite.ParseConfig(src, filepath.Base(filePath), hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		diagErr := errors.New(diags.Error())
		vp.logger.ErrorContext(ctx, "Failed to parse HCL", "path", filePath, "error", diagErr)
		return fmt.Errorf("failed to parse HCL file %q: %w", filePath, diagErr)
	}

	defaults := make(map[string]hclwrite.Tokens)
	for _, block := range varFile.Body().Blocks() {
		name := block.Labels()[0]

		defaultAttribute := block.Body().GetAttribute("default")
		if defaultAttribute != nil {
			defaults[name] = defaultAttribute.Expr().BuildTokens(nil)
		} else {
			defaults[name] = hclwrite.TokensForValue(cty.NullVal(cty.String))
		}
	}

	for _, block := range file.Body().Blocks() {
		if block.Type() != "module" {
			continue
		}

		for name, attribute := range block.Body().Attributes() {
			expr := string(attribute.Expr().BuildTokens(nil).Bytes())

			if strings.Contains(expr, "try") || strings.Contains(expr, "lookup") {
				tokens := hclwrite.TokensForIdentifier("var")
				tokens = append(tokens, &hclwrite.Token{
					Type:  hclsyntax.TokenDot,
					Bytes: []byte("."),
				})
				tokens = append(tokens, hclwrite.TokensForIdentifier(name)...)
				tokens = append(tokens, &hclwrite.Token{
					Type:  hclsyntax.TokenNotEqual,
					Bytes: []byte("!="),
				})
				tokens = append(tokens, hclwrite.TokensForIdentifier("null")...)
				tokens = append(tokens, &hclwrite.Token{
					Type:  hclsyntax.TokenQuestion,
					Bytes: []byte("?"),
				})
				tokens = append(tokens, hclwrite.TokensForIdentifier("var")...)
				tokens = append(tokens, &hclwrite.Token{
					Type:  hclsyntax.TokenDot,
					Bytes: []byte("."),
				})
				tokens = append(tokens, hclwrite.TokensForIdentifier(name)...)
				tokens = append(tokens, &hclwrite.Token{
					Type:  hclsyntax.TokenColon,
					Bytes: []byte(":"),
				})

				lookupTokens := hclwrite.TokensForIdentifier("local")
				lookupTokens = append(lookupTokens, &hclwrite.Token{
					Type:  hclsyntax.TokenDot,
					Bytes: []byte("."),
				})
				lookupTokens = append(lookupTokens, hclwrite.TokensForIdentifier("addon")...)
				lookupTokens = append(lookupTokens, &hclwrite.Token{
					Type:  hclsyntax.TokenComma,
					Bytes: []byte(","),
				})
				lookupTokens = append(lookupTokens, hclwrite.TokensForValue(cty.StringVal(name))...)
				lookupTokens = append(lookupTokens, &hclwrite.Token{
					Type:  hclsyntax.TokenComma,
					Bytes: []byte(","),
				})
				lookupTokens = append(lookupTokens, defaults[name]...)

				tokens = append(tokens, hclwrite.TokensForFunctionCall("lookup", lookupTokens)...)

				block.Body().SetAttributeRaw(name, tokens)
			}
		}
	}

	writeErr := os.WriteFile(filePath, file.Bytes(), targetFileMode)
	if writeErr != nil {
		vp.logger.ErrorContext(ctx, "Failed to write file", "path", filePath, "error", writeErr)
		return fmt.Errorf("failed to write file %q: %w", filePath, writeErr)
	}

	vp.logger.InfoContext(ctx, "Successfully wrote addon defaults", "targetPath", filePath)
	return nil
}

func (vp *VariableProcessor) syncAddonVariables(ctx context.Context, moduleName string, varFile *hclwrite.File) error {
	filePath := fmt.Sprintf(targetVariableFilePattern, moduleName)

	file := hclwrite.NewEmptyFile()

	for _, block := range varFile.Body().Blocks() {
		labels := block.Labels()

		// Skip the "enabled" variable
		if labels[0] == "enabled" {
			continue
		}

		// Update descriptionAtrribute with default value if both exist
		if descriptionAtrribute := block.Body().GetAttribute("description"); descriptionAtrribute != nil {
			if defaultAttribute := block.Body().GetAttribute("default"); defaultAttribute != nil {
				defaultValue := string(defaultAttribute.Expr().BuildTokens(nil).Bytes())
				defaultValue = strings.ReplaceAll(defaultValue, "\"", "")
				defaultValue = strings.TrimSpace(defaultValue)
				if defaultValue == "" { // If default value is empty, set it to an empty string
					defaultValue = "\"\""
				}

				descriptionValue := string(descriptionAtrribute.Expr().BuildTokens(nil).Bytes())
				descriptionValue = strings.ReplaceAll(descriptionValue, "\"", "")
				descriptionValue = strings.TrimSpace(descriptionValue)
				descriptionValue = fmt.Sprintf("%s Defaults to `%s`.", descriptionValue, defaultValue)

				block.Body().SetAttributeValue("description", cty.StringVal(descriptionValue))
			}
		}

		// Set default to null
		block.Body().SetAttributeValue("default", cty.NullVal(cty.String))

		file.Body().AppendNewline()
		file.Body().AppendBlock(block)
	}

	var buf bytes.Buffer
	execErr := vp.tmpl.Execute(&buf, &struct {
		Variables []byte
	}{
		Variables: file.Bytes(),
	})
	if execErr != nil {
		vp.logger.ErrorContext(ctx, "Failed to execute template", "path", filePath, "error", execErr)
		return fmt.Errorf("failed to execute template for file %q: %w", filePath, execErr)
	}

	writeErr := os.WriteFile(filePath, buf.Bytes(), targetFileMode)
	if writeErr != nil {
		vp.logger.ErrorContext(ctx, "Failed to write file", "path", filePath, "error", writeErr)
		return fmt.Errorf("failed to write file %q: %w", filePath, writeErr)
	}

	vp.logger.InfoContext(ctx, "Successfully wrote addon variables", "targetPath", filePath)
	return nil
}
