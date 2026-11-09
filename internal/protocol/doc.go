// Package protocol implements Homing protocol v1 (docs/protocol.md): Ed25519
// identities, device_id and fingerprints, the crypto_box envelope, SAS words and
// pairing messages. It must pass packages/protocol-js/test-vectors.json, the same
// vectors the Node and Android implementations use.
//
// Owner: Codex.
package protocol
