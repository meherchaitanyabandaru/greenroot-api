# syntax=docker/dockerfile:1.7
# GreenRoot API image: the API, the migrator and a healthcheck, on distroless
# (no shell, no package manager), running as a non-root user.
#
#   docker build -t greenroot-api:$(git rev-parse --short HEAD) .
#   docker run --rm --entrypoint /app/migrate greenroot-api up   # apply migrations
#   docker run greenroot-api                      # serve on :8080
#
# Configuration comes from the environment; in production AWS_SECRETS_ID names
# the Secrets Manager secret with every key (see greenroot-infra/deploy).

FROM golang:1.26.10-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
# timetzdata embeds the zone database (Asia/Kolkata) into the binaries.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -tags timetzdata \
      -ldflags="-s -w" -o /out/greenroot-api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -tags timetzdata -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/ /app/
# OpenAPI files (served only when DOCS_ENABLED=true).
COPY docs/swagger /app/docs/swagger
ARG VERSION=dev
ENV APP_VERSION=${VERSION} \
    HTTP_HOST=0.0.0.0 \
    HTTP_PORT=8080 \
    LOG_DIR=/tmp/greenroot-logs
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 CMD ["/app/healthcheck"]
ENTRYPOINT ["/app/greenroot-api"]
