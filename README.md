# log-webhook

Stdlib-only Go webhook that receives OTLP/JSON log exports (e.g. Collector `otlp_http` exporter with `encoding: json`) on `POST /v1/logs` and writes **one flat JSON line per log record** to STDOUT. Diagnostics go to STDERR as JSON (`"component":"log-webhook"`). `GET /health` for probes; graceful shutdown on SIGTERM.

## Output

```json
{"timestamp":"2026-11-16T11:16:38.823Z","level":"ERROR","severityNumber":17,"message":"Version \"develop\" from localstorage not valid","service":"example-frontend","scope":"example-frontend","resource":{"service.name":"example-frontend"},"attributes":{"version":"develop"}}
```

- `timestamp`: `timeUnixNano` → `observedTimeUnixNano` → receive time (RFC 3339, UTC, ms)
- `level`: `severityText`, else derived from `severityNumber`
- `message`: string body as-is, other body types JSON-serialized; `truncated:"true"` if cut
- `service` = `service.name`; `scope`/`scopeVersion`, `traceId`, `spanId` when present
- `resource`, `attributes`: flat maps, **all values strings** (arrays/kvlists as JSON)

Empty optional fields are omitted.

## Responses

`200 {}` accepted (including requests without records) · `400` invalid JSON/gzip · `405` not POST · `413` body too large (compressed or decompressed) · `415` not `application/json` or encoding not `gzip` · `500` STDOUT write failed

## Configuration

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Server port |
| `LOG_ENDPOINT` | `/v1/logs` | Log endpoint path |
| `FIELD_PREFIX` | _(empty)_ | Nest each record under this key, e.g. `otel` |
| `MAX_MESSAGE_BYTES` | `16384` | Truncate `message` beyond this; `0` disables |
| `MAX_BODY_BYTES` | `10485760` | Max request body, before and after decompression |

## Usage

```bash
go run .
go test -race ./...
docker build -t log-webhook . && docker run -p 8080:8080 log-webhook
```
