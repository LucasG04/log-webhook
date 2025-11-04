# log-webhook

A tiny HTTP webhook that accepts OTLP-compatible JSON log payloads and writes them as **line-delimited JSON** to **STDOUT**.

## Features

- `POST /v1/logs` reads JSON request body as-is and prints **one line** per log entry to STDOUT
- `GET /health` health check endpoint for monitoring
- Minimal dependencies
- Secure, small container using Distroless (nonroot)
- Configurable port and endpoint via environment variables

## Configuration

### Environment Variables

- `PORT`: Server port (default: `8080`)
- `LOG_ENDPOINT`: Log endpoint path (default: `/v1/logs`)

## Quickstart

### Run locally

```bash
# Run with defaults
go run ./main.go

# Run with custom configuration
PORT=9090 LOG_ENDPOINT=/custom/logs go run ./main.go
```

### Run tests

```bash
go test ./...
```

### Build Docker image

```bash
docker build -t log-webhook:latest .
```

### Run with Docker

```bash
# Run with defaults
docker run -p 8080:8080 log-webhook:latest

# Run with custom configuration
docker run -p 9090:9090 -e PORT=9090 -e LOG_ENDPOINT=/custom/logs log-webhook:latest
```
