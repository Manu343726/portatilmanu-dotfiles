
dotfilesd.v1«

dotfilesd.v1.KeyServiceëKeyService provides ephemeral ECDH key negotiation over insecure channels.
Keys are never exposed over the wire â€” only public keys are exchanged,
and both sides derive the same shared secret locally.

The shared secret is associated with a (session, key_id) pair and can be
used for AES-256-GCM encryption/decryption or HMAC signing/verification
on either side.¡
NegotiateKey­NegotiateKey performs an ephemeral X25519 ECDH key exchange.
The client sends its public key and a unique key_id; the daemon
generates its own ephemeral keypair, derives the shared secret,
stores it for the session+key_id, and returns its public key.
The client derives the same shared secret locally. dotfilesd.v1.NegotiateKeyRequest"!dotfilesd.v1.NegotiateKeyResponse*ž
 dotfilesd.v1.NegotiateKeyRequest)
sessiondotfilesd.v1.Session"optional‘
key_iduOpaque identifier for this key â€” both sides refer to it by this ID
when encrypting/decrypting or signing/verifying.string"optionalV
client_public_key0Client's ephemeral X25519 public key (32 bytes).bytes"optionalc
ttl_secondsCKey lifetime in seconds (0 = use daemon default, currently 15 min).int32"optional2{
!dotfilesd.v1.NegotiateKeyResponseV
server_public_key0Daemon's ephemeral X25519 public key (32 bytes).bytes"optional