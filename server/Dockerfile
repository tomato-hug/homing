# Go implementation (internal/server). Build from the repository root:
#   docker build -f server/Dockerfile -t homing-server .
FROM golang:1.24 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# Expand this list when homing-server starts importing more internal packages.
COPY cmd/homing-server ./cmd/homing-server
COPY internal/protocol ./internal/protocol
COPY internal/server ./internal/server

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/homing-server ./cmd/homing-server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/homing-server /usr/local/bin/homing-server

ENV HOST=0.0.0.0 \
    PORT=8080 \
    DB_PATH=/data/homing.sqlite
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/homing-server"]
