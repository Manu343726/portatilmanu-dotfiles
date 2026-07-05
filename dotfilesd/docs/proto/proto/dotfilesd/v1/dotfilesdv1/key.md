# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [dotfilesd.v1.KeyService](#dotfilesdv1keyservice)
    - [NegotiateKey](#negotiatekey)
- [Messages](#messages)
  - [NegotiateKeyRequest](#negotiatekeyrequest)
  - [NegotiateKeyResponse](#negotiatekeyresponse)

## Services

### dotfilesd.v1.KeyService

KeyService provides ephemeral ECDH key negotiation over insecure channels.
Keys are never exposed over the wire — only public keys are exchanged,
and both sides derive the same shared secret locally.

The shared secret is associated with a (session, key_id) pair and can be
used for AES-256-GCM encryption/decryption or HMAC signing/verification
on either side.

#### NegotiateKey

NegotiateKey performs an ephemeral X25519 ECDH key exchange.
The client sends its public key and a unique key_id; the daemon
generates its own ephemeral keypair, derives the shared secret,
stores it for the session+key_id, and returns its public key.
The client derives the same shared secret locally.

- **Request:** `dotfilesd.v1.NegotiateKeyRequest`
- **Response:** `dotfilesd.v1.NegotiateKeyResponse`


## Messages

### NegotiateKeyRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `key_id` | string | Opaque identifier for this key — both sides refer to it by this ID when encrypting/decrypting or signing/verifying. |
| `client_public_key` | bytes | Client's ephemeral X25519 public key (32 bytes). |
| `ttl_seconds` | int32 | Key lifetime in seconds (0 = use daemon default, currently 15 min). |

### NegotiateKeyResponse

| Field | Type | Description |
|-------|------|-------------|
| `server_public_key` | bytes | Daemon's ephemeral X25519 public key (32 bytes). |

