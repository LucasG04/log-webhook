# ---- Build stage ----
FROM golang:1.26 AS build
WORKDIR /src
COPY . .

# Build a static binary (no CGO) for minimal container bases
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/log-webhook .

# ---- Runtime stage (distroless) ----
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
USER nonroot:nonroot
EXPOSE 8080
COPY --from=build /out/log-webhook /log-webhook

ENTRYPOINT ["/log-webhook"]