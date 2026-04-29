# Archio

Archio is a Go-based HTTP server application.

## Prerequisites

- Go (1.21+)
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

To start the server during local development:

```bash
go run cmd/api/main.go
```

If your `.env` specifies `RUN_MODE=development`, a `statsviz` visualization dashboard will become available.

- **Health Check endpoints**: `http://127.0.0.1:3000/health`
- **Metrics Dashboard**: `http://127.0.0.1:3000/debug/statsviz/`

## Building from Source

To compile the application into a standalone executeable binary:

```bash
go build -o archio-api cmd/api/main.go
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
