.PHONY: build run test docker-build migrate-up migrate-down lint clean deploy-prod deploy-configs deploy-server

NS ?= agent-backend

# Binary name
BINARY=server

# Build the server binary
build:
	go build -o bin/$(BINARY) ./cmd/server

# Run locally (requires environment variables)
run:
	go run ./cmd/server

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Build Docker image
docker-build:
	docker build -t agent-backend:latest .

# Run database migrations up
migrate-up:
	go run -tags 'postgres' github.com/pressly/goose/v3/cmd/goose@latest -dir internal/storage/postgres/migrations postgres "$(DATABASE_DSN)" up

# Run database migrations down
migrate-down:
	go run -tags 'postgres' github.com/pressly/goose/v3/cmd/goose@latest -dir internal/storage/postgres/migrations postgres "$(DATABASE_DSN)" down

# Lint code
lint:
	golangci-lint run

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

deploy-prod: deploy-configs deploy-server

deploy-configs:
	kubectl -n $(NS) apply -f deploy/prod

deploy-server:
	kubectl -n $(NS) apply -f deploy/01_server.yaml
	kubectl -n $(NS) rollout status deployment/server --timeout=300s
