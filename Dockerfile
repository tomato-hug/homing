# syntax=docker/dockerfile:1.7

FROM node:22-bookworm-slim AS console
WORKDIR /src
COPY console/package.json console/package-lock.json ./console/
RUN cd console && npm ci
COPY console ./console
COPY web/console ./web/console
RUN cd console && npm run build \
    && rm -rf ../web/console/assets ../web/console/sponsor \
    && mkdir -p ../web/console \
    && cp dist/index.html dist/favicon.svg ../web/console/ \
    && cp -R dist/assets ../web/console/ \
    && if [ -d dist/sponsor ]; then cp -R dist/sponsor ../web/console/; fi

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=console /src/web/console ./web/console
RUN CGO_ENABLED=0 go build -tags headless -trimpath \
      -ldflags "-s -w -X main.version=docker" \
      -o /out/homing ./cmd/homing \
    && CGO_ENABLED=0 go build -tags headless -trimpath \
      -ldflags "-s -w" \
      -o /out/homing-server ./cmd/homing-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/homing /usr/local/bin/homing
COPY --from=build /out/homing-server /usr/local/bin/homing-server
VOLUME ["/data"]
EXPOSE 18787
ENTRYPOINT ["/usr/local/bin/homing"]
CMD ["serve", "--addr", "0.0.0.0:18787", "--data-dir", "/data"]
