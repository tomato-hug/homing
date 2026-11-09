import { createHomingServer } from "./src/server.mjs";

const port = Number(process.env.PORT ?? 8080);
const host = process.env.HOST ?? "0.0.0.0";
const dbPath = process.env.DB_PATH ?? new URL("./data/homing.sqlite", import.meta.url).pathname;

const server = await createHomingServer({ port, host, dbPath });
console.log(`homing-server listening on ws://${host}:${server.port}/v1/ws`);
console.log(`sqlite: ${dbPath}`);

const shutdown = async (signal) => {
  console.log(`[server] ${signal}, shutting down`);
  await server.close();
  process.exit(0);
};
process.on("SIGINT", () => void shutdown("SIGINT"));
process.on("SIGTERM", () => void shutdown("SIGTERM"));
