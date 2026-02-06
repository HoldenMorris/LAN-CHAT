# Plan: Optional Password-Based Encryption (`--pass` flag)

## Context

LAN-CHAT currently sends all messages and files as plaintext over TCP. This feature adds an optional `--pass="12345"` CLI flag that encrypts communication between peers sharing the same password. Peers without matching passwords (or no password) fall back to unencrypted mode automatically.

**All changes are in `main.go`** (single-file architecture). No new external dependencies - uses Go stdlib crypto.

## Encryption Scheme

- **Algorithm**: AES-256-GCM (authenticated encryption)
- **Key derivation**: SHA-256 hash of password → 32-byte key
- **Nonce**: 12 random bytes per message (prepended to ciphertext)
- **Encoding**: Base64 for wire format (line-based protocol safe)
- **Verification**: SHA-256 of `"LAN-CHAT-VERIFY:" + password` exchanged via TCP handshake

## New Protocol Messages

| Prefix | Purpose |
|---|---|
| `VERIFY:<fingerprint>\n` | Password verification handshake (responds `VMATCH` or `VNOMATCH`) |
| `ECHAT:<sender>:<base64-encrypted>\n` | Encrypted chat message |
| `EFILE:<filename>\n` + base64 blob | Encrypted file transfer |

## Behavior Matrix

| Peer A | Peer B | Result |
|---|---|---|
| No password | No password | Plain text (unchanged) |
| `--pass=X` | `--pass=X` | Encrypted + lock icon |
| `--pass=X` | `--pass=Y` | Falls back to plain text, no lock |
| `--pass=X` | No password | Falls back to plain text, no lock |
