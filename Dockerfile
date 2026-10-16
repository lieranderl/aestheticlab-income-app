# syntax=docker/dockerfile:1
# Aesthetic Lab Kassa (cmd/web) for Cloud Run. Adapted from datastar-webapp's
# Dockerfile.web: Bun builds the CSS and icons, Go builds a static binary,
# distroless runs it as non-root.
#
#   docker build -t aestheticlab-income:local .

FROM --platform=$BUILDPLATFORM docker.io/oven/bun:1.4.2@sha256:9114c058aeae42162ee16dd5084b95fe9473970bb6bcb5b232ab1630f0546895 AS assets
WORKDIR /src
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend ./frontend
COPY internal ./internal
COPY static ./static
RUN bun run assets

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=assets /src/static/app.css /src/static/
COPY --from=assets /src/internal/views/icons_gen.go /src/internal/views/icons_gen.go
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go tool templ generate && \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -trimpath -ldflags "-s -w" -o /out/web ./cmd/web

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/web /web
LABEL org.opencontainers.image.title="Aesthetic Lab Kassa" \
      org.opencontainers.image.source="https://github.com/lieranderl/aestheticlab-income-app"
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/web"]
