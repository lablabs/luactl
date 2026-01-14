# luactl

A command-line tool for managing [terraform-aws-eks-universal-addon](https://github.com/lablabs/terraform-aws-eks-universal-addon) Terraform template.

## Overview

`luactl` helps manage variables between Terraform modules, specifically designed to work with the terraform-aws-eks-universal-addon. The primary functionality is synchronizing variables from nested addon modules to the root module, ensuring consistency across your infrastructure code.

## Features

- **Variable Synchronization**: Automatically syncs variable definitions from nested addon submodules to the root module
- **Default Management**: Updates default values in addon files
- **Description Enhancement**: Augments variable descriptions with their default values

## Installation

### Prerequisites

- Go 1.24.3 or higher

### From source

```bash
# Clone the repository
git clone https://github.com/lablabs/luactl.git

# Navigate to the project directory
cd luactl

# Build the binary
go build -o luactl

# Optional: Move to a directory in your PATH
sudo mv luactl /usr/local/bin/
```

### From binary releases

Download the appropriate binary for your platform from the [releases page](https://github.com/lablabs/luactl/releases) or by running `GOPRIVATE=github.com/lablabs/luactl go install github.com/lablabs/luactl@latest`.

## Usage

### Basic Commands

```bash
# Get help information
luactl --help

# Run the sync command with default settings
luactl sync

# Run with a custom modules directory
luactl sync --modules-dir /path/to/modules
```

### Sync Command

The sync command reads `variables.tf` files from addon modules within the `.terraform/modules` directory (by default) and generates corresponding `variables-<addon-name>.tf` files in the current directory.

```bash
luactl sync [flags]
```

#### Flags

- `-d, --modules-dir string`: Directory containing Terraform modules (default ".terraform/modules")
- `-l, --log-level string`: Set logging level (debug, info, warn, error) (default "info")
- `-t, --timeout duration`: Global command timeout (default 2m0s)

## Development

### Requirements

- Go 1.24.3
- golangci-lint 2.1.6
- pre-commit 4.2.0

### Setting up the development environment

```bash
# Install dependencies
go mod download

# Install pre-commit hooks
pre-commit install
```

### Running tests

```bash
go test ./...
```

### Building the binary

```bash
go build
```
