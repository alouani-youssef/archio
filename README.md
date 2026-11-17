# Archio

Archio is a Go-based HTTP server application.

## Prerequisites

- Go (1.25.6+)
- Docker (optional, for containerization)

## Getting Started

### 1. Setup Environment

First, duplicate the example environment variables file and update it with your desired configuration:

```bash
cp .env.example .env
```

### 2. Install Dependencies

Download the required Go modules:

```bash
go mod download
```

## Running Locally

Start the local dependencies with Docker Compose:

```bash
docker compose up -d
```

The Compose stack provides PostgreSQL with the `vector` extension enabled, Redis, and RabbitMQ. PostgreSQL data, Redis data, and RabbitMQ data persist in named Docker volumes. RabbitMQ's management UI is available at `http://127.0.0.1:15672`.

Note: the group, API key and object version schema change requires a fresh database volume. Reset local data with `docker compose down -v` before starting the stack.

Copy `.env.example` to `.env` to use the matching local connection settings, then start the server during local development:

```bash
go run ./cmd/api
```

At startup, Archio opens PostgreSQL, Redis, and RabbitMQ, enables pgvector if needed, and applies the Ent schema. `/health` is a readiness check: it returns HTTP 200 when all three services are reachable and HTTP 503 otherwise, with a per-service status and no raw connection errors. If `.env` specifies `RUN_MODE=development`, a `statsviz` visualization dashboard is available.

After changing an Ent schema, regenerate the client and migration definitions:

```bash
go generate ./internal/ent
```

Stop the dependencies with `docker compose down`. To remove their persisted data as well, run `docker compose down -v`.

- **Health Check endpoints**: `http://127.0.0.1:3000/health`
- **Metrics Dashboard**: `http://127.0.0.1:3000/debug/statsviz/`

## Building from Source

To compile the application into a standalone executeable binary:

```bash
go build -o archio-api ./cmd/api
```

Run the binary:
```bash
./archio-api
```

## Running with Docker

A `Dockerfile` is provided that uses a multi-stage build to package the application efficiently.

### 1. Build the Docker Image

```bash
docker build -t archio-api .
```

### 2. Start the Docker Container

Provide your `.env` file to the container to pass the required configuration, and bind the application port to your host machine:

```bash
docker run -p 3000:3000 --env-file .env archio-api
```
