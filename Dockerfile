ARG GO_IMAGE=golang:1.26-alpine
ARG RUNTIME_IMAGE=debian:12-slim
FROM ${GO_IMAGE} AS build

WORKDIR /src

COPY go.mod go.sum ./
COPY third_party ./third_party
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -tags nogui -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" -o /out/magpie . \
    && mkdir -p /config/home /config/cache /config/data /config/state

FROM ${RUNTIME_IMAGE} AS enterprise-server

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 65532 nonroot \
    && useradd -u 65532 -g nonroot -d /config/home -s /bin/bash nonroot

COPY --from=build /out/magpie /magpie
RUN ln -s /magpie /usr/local/bin/magpie
USER nonroot
COPY --from=build --chown=65532:65532 /config /config

ENV HOME=/config/home \
    XDG_CONFIG_HOME=/config \
    XDG_CACHE_HOME=/config/cache \
    XDG_DATA_HOME=/config/data \
    XDG_STATE_HOME=/config/state \
    MAGPIE_ADDR=0.0.0.0:3425 \
    MAGPIE_EDITION=enterprise-server

VOLUME /config

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/magpie", "healthcheck"]

EXPOSE 3425 3430

ENTRYPOINT ["/magpie"]
CMD ["serve", "--web=0.0.0.0:3430"]
