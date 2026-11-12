# Variables
APP_NAME := tfkycv
IMAGE_NAME := ghcr.io/threefoldtech/grid_kyc_verifier
MAIN_PATH := cmd/api/main.go
SWAGGER_GENERAL_API_INFO_PATH := internal/handlers/handlers.go
DOCKER_COMPOSE := docker compose

# Go related variables
GOBASE := $(shell pwd)
GOBIN := $(GOBASE)/bin
GOFILES := $(wildcard *.go)

# Git related variables
GIT_COMMIT := $(shell git rev-parse --short HEAD)
VERSION := $(shell git describe --tags --always)

# Build flags
LDFLAGS := -X github.com/threefoldtech/tf-kyc-verifier/internal/build.Version=$(VERSION)

.PHONY: all build clean test coverage lint swagger run docker-build docker-up docker-down help

# Default target
all: clean build

# Build the application
build:
	@echo "Building $(APP_NAME)..."
	@go build -ldflags "$(LDFLAGS)" -o $(GOBIN)/$(APP_NAME) $(MAIN_PATH)

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(GOBIN)
	@go clean

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Run tests with coverage
coverage:
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out
	@rm coverage.out

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run

# Generate swagger documentation
swagger:
	@echo "Generating Swagger documentation..."
	@export PATH=$PATH:$(go env GOPATH)/bin
	@swag init -g $(SWAGGER_GENERAL_API_INFO_PATH) --output api/docs

# Run the application locally
run: swagger build
	@echo "Running $(APP_NAME)..."
	@set -o allexport; . ./.app.env; set +o allexport; $(GOBIN)/$(APP_NAME)

# Build docker image
docker-build:
	@echo "Building Docker image..."
	@docker build -t $(IMAGE_NAME):$(VERSION) .

# Start docker compose services
docker-up:
	@echo "Starting Docker services..."
	@$(DOCKER_COMPOSE) up --build -d

# Stop docker compose services
docker-down:
	@echo "Stopping Docker services..."
	@$(DOCKER_COMPOSE) down

# Start development environment
dev: swagger docker-up
	@echo "Starting development environment..."
	@$(DOCKER_COMPOSE) logs -f api

# Update dependencies
deps-update:
	@echo "Updating dependencies..."
	@go get -u ./...
	@go mod tidy

# Verify dependencies
deps-verify:
	@echo "Verifying dependencies..."
	@go mod verify

# Check for security vulnerabilities
security-check:
	@echo "Checking for security vulnerabilities..."
	@gosec ./...

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Show help
help:
	@echo "Available targets:"
	@echo "  make              : Build the application after cleaning"
	@echo "  make build        : Build the application"
	@echo "  make clean        : Clean build artifacts"
	@echo "  make test         : Run tests"
	@echo "  make coverage     : Run tests with coverage report"
	@echo "  make lint         : Run linter"
	@echo "  make swagger      : Generate Swagger documentation"
	@echo "  make run          : Run the application locally"
	@echo "  make docker-build : Build Docker image"
	@echo "  make docker-up    : Start Docker services"
	@echo "  make docker-down  : Stop Docker services"
	@echo "  make dev          : Start development environment"
	@echo "  make deps-update  : Update dependencies"
	@echo "  make deps-verify  : Verify dependencies"
	@echo "  make security-check: Check for security vulnerabilities"
	@echo "  make fmt          : Format code"
	@echo "  make install-tools: Install development tools"

# Install development tools
.PHONY: install-tools
install-tools:
	@echo "Installing development tools..."
	@go install github.com/swaggo/swag/cmd/swag@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/securego/gosec/v2/cmd/gosec@latest
