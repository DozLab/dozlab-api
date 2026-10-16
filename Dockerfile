# The API and its migration command (cmd/migrate, run by the dozlab chart before each install and
# upgrade). Static binaries on distroless, as the non-root user the chart runs it as (65532).
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/migrate ./cmd/migrate

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/api /out/migrate /usr/local/bin/
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]
