# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/controller/ cmd/controller/
COPY internal/controller/ internal/controller/

# Build the controller
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o controller cmd/controller/main.go

# Final stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/controller .

USER 65532:65532

ENTRYPOINT ["/controller"]
