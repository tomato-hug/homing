# Security

Homing is currently Beta software. Security fixes target the latest development and Beta versions; older snapshots have no guaranteed maintenance window.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting interface when it is enabled for this repository: **Security → Report a vulnerability**. If that option is unavailable, open an issue requesting a private contact channel, with no exploit details, keys, tokens or conversation content. Do not attach a local data directory or full diagnostic archive to a public issue.

A useful private report identifies the affected commit / version, deployment topology, prerequisites, a minimal synthetic reproduction and the impact. There is currently no published response-time commitment or independent security-audit claim.

## Trust boundaries

- Devices encrypt message payloads before sending them through the relay. The relay still sees routing metadata, device public keys, pairing relationships, timestamps, encrypted envelopes and registered push tokens. This is not a promise of metadata anonymity.
- The computer service has access to local Agent conversations and projects. A paired phone can invoke permitted operations on that computer, including replies and explicitly confirmed action dispatches. Pair only devices you trust, compare verification words / fingerprints, and revoke lost devices.
- End-to-end encryption does not protect a compromised phone, computer, Agent runtime or local backup. Protect local identity keys, configuration, databases and exported transcripts.
- Use trusted HTTPS/WSS certificates. Keep the computer's management API and console private. A successful health check is not evidence that pairing, encrypted RPC, reconnect and permission boundaries are all correct.
- Agent tools execute with the permissions of their configured runtime. Review action confirmations and keep tests in disposable projects. Homing is not an operating-system sandbox for Agent tools.
- Link handling in the mobile browser is constrained, but remote pages are still third-party content. Do not place credentials or pairing tokens in shared links.

The self-hosting guide documents relay retention, push behavior and backup requirements. Review those settings for your deployment before sharing the relay with additional users.
