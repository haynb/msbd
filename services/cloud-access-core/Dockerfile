# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.24.6
FROM golang:${GO_VERSION}-bookworm AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOEXPERIMENT=arenas GOOS=linux GOARCH=amd64 go build -trimpath -o /workspace/bin/cloud-access-core ./cmd/cloud-access-core

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app
COPY --from=builder /workspace/bin/cloud-access-core /app/cloud-access-core
USER nonroot:nonroot
EXPOSE 8080 9090 9091
ENTRYPOINT ["/app/cloud-access-core"]
