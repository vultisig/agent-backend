# agent-backend

AI Chat Agent backend service for Vultisig mobile apps. This service handles natural language conversations using Anthropic Claude and coordinates with existing Vultisig plugins (app-recurring, feeplugin) via the verifier.

## Prerequisites

- Go 1.25+
- PostgreSQL 14+
- Redis 6+
- Docker (optional, for containerized deployment)

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SERVER_HOST` | No | `0.0.0.0` | Server bind address |
| `SERVER_PORT` | No | `8080` | Server port |
| `JWT_SECRET` | Yes | - | Secret for JWT token signing |
| `DATABASE_DSN` | Yes | - | PostgreSQL connection string |
| `REDIS_URI` | Yes | - | Redis connection URI |
| `ANTHROPIC_API_KEY` | Yes | - | Anthropic Claude API key |
| `ANTHROPIC_MODEL` | No | `claude-sonnet-4-20250514` | Claude model to use |
| `VERIFIER_URL` | Yes | - | Verifier service base URL |
| `LOG_FORMAT` | No | `json` | Log format (`json` or `text`) |

## Running Locally

1. Set required environment variables:

```bash
export JWT_SECRET="your-jwt-secret"
export DATABASE_DSN="postgres://user:pass@localhost:5432/agent?sslmode=disable"
export REDIS_URI="redis://localhost:6379"
export ANTHROPIC_API_KEY="sk-ant-..."
export VERIFIER_URL="http://localhost:8080"
```

2. Run the server:

```bash
make run
```

Or build and run:

```bash
make build
./bin/server
```

## Docker

Build the Docker image:

```bash
make docker-build
```

Run with Docker:

```bash
docker run -p 8080:8080 \
  -e JWT_SECRET="your-jwt-secret" \
  -e DATABASE_DSN="postgres://..." \
  -e REDIS_URI="redis://..." \
  -e ANTHROPIC_API_KEY="sk-ant-..." \
  -e VERIFIER_URL="http://verifier:8080" \
  agent-backend:latest
```

## Database Migrations

Run migrations:

```bash
export DATABASE_DSN="postgres://user:pass@localhost:5432/agent?sslmode=disable"
make migrate-up
```

Rollback:

```bash
make migrate-down
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Health check |
| `POST` | `/agent/conversations` | Create conversation |
| `POST` | `/agent/conversations/list` | List conversations |
| `POST` | `/agent/conversations/:id` | Get conversation |
| `POST` | `/agent/conversations/:id/messages` | Send message |
| `DELETE` | `/agent/conversations/:id` | Delete conversation |

## Development

Run tests:

```bash
make test
```

Run linter:

```bash
make lint
```

## Architecture

```
cmd/server/          # Main entrypoint
internal/
  api/               # HTTP handlers and middleware
  service/           # Business logic layer
  storage/postgres/  # PostgreSQL repositories + migrations
  cache/redis/       # Redis caching
  ai/anthropic/      # Anthropic Claude integration
  config/            # Configuration loading
  types/             # Shared types
```
