import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import Database from "better-sqlite3";

const MAX_QUEUE = 500;
const QUEUE_TTL_MS = 7 * 24 * 60 * 60 * 1000;

export class HomingDb {
  constructor(path) {
    if (path !== ":memory:") {
      mkdirSync(dirname(path), { recursive: true });
    }
    this.db = new Database(path);
    this.db.pragma("journal_mode = DELETE");
    this.db.pragma("foreign_keys = ON");
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS devices (
        device_id TEXT PRIMARY KEY,
        pub TEXT NOT NULL,
        first_seen INTEGER NOT NULL,
        last_seen INTEGER NOT NULL
      );
      CREATE TABLE IF NOT EXISTS pairings (
        device_a TEXT NOT NULL,
        device_b TEXT NOT NULL,
        created_at INTEGER NOT NULL,
        PRIMARY KEY (device_a, device_b)
      );
      CREATE TABLE IF NOT EXISTS queue (
        id TEXT PRIMARY KEY,
        from_id TEXT NOT NULL,
        to_id TEXT NOT NULL,
        envelope TEXT NOT NULL,
        ts INTEGER NOT NULL
      );
      CREATE INDEX IF NOT EXISTS queue_to_ts ON queue (to_id, ts);
      CREATE TABLE IF NOT EXISTS push_tokens (
        device_id TEXT NOT NULL,
        provider TEXT NOT NULL,
        token TEXT NOT NULL,
        updated_at INTEGER NOT NULL,
        PRIMARY KEY (device_id, provider)
      );
    `);
    this.statement = {
      getDevice: this.db.prepare("SELECT * FROM devices WHERE device_id = ?"),
      insertDevice: this.db.prepare(
        "INSERT INTO devices (device_id, pub, first_seen, last_seen) VALUES (?, ?, ?, ?)",
      ),
      touchDevice: this.db.prepare("UPDATE devices SET last_seen = ? WHERE device_id = ?"),
      pairing: this.db.prepare(
        "SELECT 1 FROM pairings WHERE device_a = ? AND device_b = ?",
      ),
      insertPairing: this.db.prepare(
        "INSERT OR REPLACE INTO pairings (device_a, device_b, created_at) VALUES (?, ?, ?)",
      ),
      deletePairing: this.db.prepare(
        "DELETE FROM pairings WHERE device_a = ? AND device_b = ?",
      ),
      insertQueue: this.db.prepare(
        "INSERT OR REPLACE INTO queue (id, from_id, to_id, envelope, ts) VALUES (?, ?, ?, ?, ?)",
      ),
      queued: this.db.prepare(
        "SELECT envelope FROM queue WHERE to_id = ? ORDER BY ts ASC LIMIT 500",
      ),
      deleteQueued: this.db.prepare("DELETE FROM queue WHERE id = ? AND to_id = ?"),
      queueCount: this.db.prepare("SELECT COUNT(*) AS count FROM queue WHERE to_id = ?"),
      oldestQueue: this.db.prepare(
        "SELECT id FROM queue WHERE to_id = ? ORDER BY ts ASC LIMIT ?",
      ),
      pruneQueue: this.db.prepare("DELETE FROM queue WHERE to_id = ? AND ts < ?"),
      deletePairQueue: this.db.prepare(
        "DELETE FROM queue WHERE (from_id = ? AND to_id = ?) OR (from_id = ? AND to_id = ?)",
      ),
      savePush: this.db.prepare(
        `INSERT INTO push_tokens (device_id, provider, token, updated_at)
         VALUES (?, ?, ?, ?)
         ON CONFLICT(device_id, provider) DO UPDATE SET token = excluded.token, updated_at = excluded.updated_at`,
      ),
      pushTokens: this.db.prepare(
        "SELECT provider, token FROM push_tokens WHERE device_id = ?",
      ),
    };
  }

  getDevice(deviceId) {
    return this.statement.getDevice.get(deviceId);
  }

  registerDevice(deviceId, pub, now = Date.now()) {
    const existing = this.getDevice(deviceId);
    if (!existing) {
      this.statement.insertDevice.run(deviceId, pub, now, now);
      return true;
    }
    if (existing.pub !== pub) return false;
    this.statement.touchDevice.run(now, deviceId);
    return true;
  }

  isPaired(a, b) {
    const [left, right] = [a, b].sort();
    return Boolean(this.statement.pairing.get(left, right));
  }

  createPairing(a, b, now = Date.now()) {
    const [left, right] = [a, b].sort();
    this.statement.insertPairing.run(left, right, now);
  }

  revokePairing(a, b) {
    const [left, right] = [a, b].sort();
    this.statement.deletePairing.run(left, right);
    this.statement.deletePairQueue.run(a, b, b, a);
  }

  enqueue(envelope, now = Date.now()) {
    const transaction = this.db.transaction(() => {
      this.statement.pruneQueue.run(envelope.to, now - QUEUE_TTL_MS);
      const count = this.statement.queueCount.get(envelope.to).count;
      if (count >= MAX_QUEUE) {
        const remove = this.statement.oldestQueue.all(envelope.to, count - MAX_QUEUE + 1);
        for (const row of remove) {
          this.db.prepare("DELETE FROM queue WHERE id = ?").run(row.id);
        }
      }
      this.statement.insertQueue.run(
        envelope.id,
        envelope.from,
        envelope.to,
        JSON.stringify(envelope),
        now,
      );
    });
    transaction();
  }

  queuedFor(deviceId) {
    return this.statement.queued.all(deviceId).map((row) => JSON.parse(row.envelope));
  }

  ack(deviceId, id) {
    return this.statement.deleteQueued.run(id, deviceId).changes > 0;
  }

  queueCount(deviceId) {
    return this.statement.queueCount.get(deviceId).count;
  }

  savePushToken(deviceId, provider, token, now = Date.now()) {
    this.statement.savePush.run(deviceId, provider, token, now);
  }

  pushTokens(deviceId) {
    return this.statement.pushTokens.all(deviceId);
  }

  close() {
    this.db.close();
  }
}
