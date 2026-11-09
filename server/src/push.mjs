const SUPPORTED_PROVIDERS = new Set([
  "fcm",
  "oppo",
  "xiaomi",
  "huawei",
  "honor",
  "vivo",
]);

export { SUPPORTED_PROVIDERS };

export class PushDispatcher {
  constructor({ db, logger = console, fetchImpl = globalThis.fetch, debounceMs = 30_000 }) {
    this.db = db;
    this.logger = logger;
    this.fetchImpl = fetchImpl;
    this.debounceMs = debounceMs;
    this.lastWake = new Map();
  }

  async wake(deviceId) {
    const now = Date.now();
    const last = this.lastWake.get(deviceId) ?? 0;
    if (now - last < this.debounceMs) return { skipped: "debounced" };
    this.lastWake.set(deviceId, now);
    const tokens = this.db.pushTokens(deviceId);
    if (tokens.length === 0) return { skipped: "no_token" };

    const results = [];
    for (const item of tokens) {
      if (item.provider !== "fcm") {
        this.logger.info?.(`[push] ${item.provider} adapter not configured for ${deviceId}`);
        results.push({ provider: item.provider, skipped: true });
        continue;
      }
      results.push(await this.sendFcm(item.token));
    }
    return { results };
  }

  async sendFcm(token) {
    const key = process.env.FCM_SERVER_KEY;
    if (!key || typeof this.fetchImpl !== "function") {
      return { provider: "fcm", skipped: true, reason: "FCM_SERVER_KEY not configured" };
    }
    try {
      const response = await this.fetchImpl("https://fcm.googleapis.com/fcm/send", {
        method: "POST",
        headers: {
          Authorization: `key=${key}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          to: token,
          priority: "high",
          notification: { title: "Homing", body: "有新消息" },
          data: { type: "wake" },
        }),
      });
      return { provider: "fcm", ok: response.ok, status: response.status };
    } catch (error) {
      this.logger.warn?.(`[push] FCM failed: ${error.message}`);
      return { provider: "fcm", ok: false, error: error.message };
    }
  }
}
