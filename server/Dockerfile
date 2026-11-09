FROM node:22-bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 make g++ \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY packages/protocol-js/package.json packages/protocol-js/package-lock.json packages/protocol-js/
COPY packages/protocol-js/src packages/protocol-js/src
RUN cd packages/protocol-js && npm ci --omit=dev

COPY server/package.json server/package-lock.json server/
RUN cd server && npm ci --omit=dev

COPY server/index.mjs server/
COPY server/src server/src

ENV NODE_ENV=production
ENV PORT=8080
ENV HOST=0.0.0.0
ENV DB_PATH=/data/homing.sqlite

VOLUME ["/data"]
EXPOSE 8080

CMD ["node", "server/index.mjs"]
