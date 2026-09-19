# SPDX-FileCopyrightText: Copyright (c) 2026 the osg authors
# SPDX-License-Identifier: MIT
#
# Build from a workspace that has sibling checkouts (CI does this):
#   docker build -f Dockerfile -t osg-gateway:local .
# Context = parent of osg-gateway/ (contains osg-core, osg-runtime, …).

FROM golang:1.27-bookworm AS build
WORKDIR /src
ENV GOWORK=off
COPY osg-core ./osg-core
COPY osg-providers ./osg-providers
COPY osg-runtime ./osg-runtime
COPY osg-proxy ./osg-proxy
COPY osg-driver ./osg-driver
COPY osg-sdk ./osg-sdk
COPY slogx ./slogx
COPY osg-gateway ./osg-gateway
WORKDIR /src/osg-gateway
RUN CGO_ENABLED=0 go build -o /out/osg-gateway ./cmd/osg-gateway

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/osg-gateway /usr/local/bin/osg-gateway
LABEL org.opencontainers.image.title="osg-gateway" \
      org.opencontainers.image.description="osg control-plane gateway" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/zorneth/osg-gateway"
ENV OSG_GATEWAY_DATA=/var/lib/osg-gateway
VOLUME ["/var/lib/osg-gateway"]
EXPOSE 7443
HEALTHCHECK --interval=15s --timeout=3s --start-period=3s --retries=5 \
  CMD curl -fsS http://127.0.0.1:7443/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/osg-gateway"]
CMD ["--listen", "0.0.0.0:7443", "--data-dir", "/var/lib/osg-gateway"]
