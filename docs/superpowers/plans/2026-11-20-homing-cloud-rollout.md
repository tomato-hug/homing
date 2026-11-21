# Homing cloud rollout and Android acceptance — 2026-11-20

Spec: user's 2026-11-20 request, docs/self-host.md, docs/protocol.md. Deploy the existing encrypted cloud relay to the user's Tencent Lighthouse instance and validate Android connectivity and meeting/action workflows.

## Constraints

- Keep the shared homing-v3 branch and existing worktree; no stash, reset or branch switch. Build and test a clean candidate snapshot under /tmp.
- Preserve unrelated uncommitted work and existing server applications. Use a dedicated Homing service, loopback listener and persistent data directory behind the existing TLS proxy.
- All test conversations are newly created in /tmp; never inject into existing agent sessions. Keep local 8787/18787 running.
- Android installs use the debug signing identity. Phone/mobile-data results require an actual device; automated public-relay tests are recorded separately.
- Do not expose credentials in scripts, source, reports or command output.

## Task 1: relay regression coverage

Review and finish the existing uncommitted changes in cmd/homing-server and internal/server: trusted-proxy client identity and bounded pairing invitations. Add meaningful regression tests, reproduce the old behavior in an isolated snapshot and verify the candidate. Global gates: go build ./..., headless tests/vet, gitleaks before commit.

## Task 2: deployable service package

Prepare a versioned Linux binary and checksum, systemd service, reverse-proxy example and a resumable installation procedure. Validate artifact architecture, shell syntax and service behavior. Reuse the actual server's TLS proxy and verify domain/certificate before public launch.

## Task 3: public relay end-to-end acceptance

Use newly generated identities to test authenticated handshake, token/code pairing, rejection of unpaired traffic, encrypted delivery, offline replay/ack, reconnection and revocation through the public TLS endpoint. Verify backend is loopback-only and service survives restart. Review the final changes, commit/push only the task paths, then verify the deployed binary matches the tested revision.

## Task 4: Android physical acceptance

When USB/ADB is available, build/install the debug APK, connect an isolated local host to the public relay, pair the phone and verify conference chat/@Agent/status/action dispatch. Test Wi-Fi/mobile switching, background/foreground and reconnect with actual transport evidence. Record any missing device, network or push-provider prerequisite honestly.

## Review focus

Forwarded IP spoofing and trust boundaries; pairing invitation replacement and rejected old tokens; existing service preservation; public TLS validation without bypasses; no secrets or real conversations in artifacts; distinction between automated relay tests and actual Android/mobile-data evidence.
