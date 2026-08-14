# syntax=docker/dockerfile:1.10

ARG CADDY_VERSION=2.11.4

FROM --platform=$BUILDPLATFORM caddy:${CADDY_VERSION}-builder-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod,id=hideck-caddy-dns-gomod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,id=hideck-caddy-dns-gobuild,sharing=locked \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH xcaddy build \
    --with github.com/caddy-dns/cloudflare@v0.2.4 \
    --with github.com/caddy-dns/alidns@v1.0.29 \
    --with github.com/caddy-dns/tencentcloud@v0.4.3 \
    --with github.com/caddy-dns/route53@v1.6.2

FROM caddy:${CADDY_VERSION}-alpine
LABEL org.opencontainers.image.title="HiDeck Caddy DNS" \
      org.opencontainers.image.description="Caddy with Cloudflare, AliDNS, Tencent Cloud DNS and Route53 modules" \
      org.opencontainers.image.source="https://github.com/yibaiba/hideck"
COPY --from=builder /usr/bin/caddy /usr/bin/caddy
