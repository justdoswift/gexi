# gexi-relay in a small image: a static binary on distroless, no shell, no
# package manager, running as a non-root user.
#
# Build from the repository root:
#   docker build -f deploy/relay/Dockerfile -t gexi-relay .
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gexi-relay ./cmd/gexi-relay

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/gexi-relay /usr/local/bin/gexi-relay
WORKDIR /data
ENV GEXI_RELAY_CONFIG=/data/config.json
USER nonroot:nonroot
EXPOSE 47824 47825
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/gexi-relay"]
CMD ["serve", "-config", "/data/config.json"]
