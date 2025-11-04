# ---- Build stage ----
FROM golang:1.24 AS build
WORKDIR /src

# Pre-cache deps
COPY go.mod ./
RUN go mod download

# Copy source
COPY . .

# Build a static binary (no CGO) for minimal container bases
ENV CGO_ENABLED=0
RUN go test ./... && \
    go build -trimpath -ldflags="-s -w" -o /out/log-webhook ./main.go

# ---- Runtime stage (distroless) ----
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
USER nonroot:nonroot
EXPOSE 8080
COPY --from=build /out/log-webhook /log-webhook

ENTRYPOINT ["/log-webhook"]