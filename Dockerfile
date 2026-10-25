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

# Builds natively for the target (amd64 or arm64/Graviton) via buildx:
#   docker buildx build --platform linux/arm64 ...
FROM --platform=$BUILDPLATFORM golang:1.26.10-bookworm AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
# timetzdata embeds the zone database (Asia/Kolkata) into the binaries.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    export CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH && \
    go build -trimpath -tags timetzdata -ldflags="-s -w" -o /out/greenroot-api ./cmd/api && \
    go build -trimpath -tags timetzdata -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

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
