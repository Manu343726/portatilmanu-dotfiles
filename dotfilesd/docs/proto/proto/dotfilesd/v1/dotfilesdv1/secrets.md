# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [dotfilesd.v1.SecretsService](#dotfilesdv1secretsservice)
    - [GetSecret](#getsecret)
- [Messages](#messages)
  - [GetSecretRequest](#getsecretrequest)
  - [GetSecretResponse](#getsecretresponse)

## Services

### dotfilesd.v1.SecretsService

SecretsService provides encrypted secret retrieval for plugins.
Secrets are loaded from a YAML file on daemon startup and are only
accessible to the plugin they belong to. Values are encrypted with
AES-256-GCM using an ECDH-negotiated shared key (key_id="secrets")
so the plugin never receives plaintext over the wire and decrypts
only at the point of use.

#### GetSecret

GetSecret returns a secret value encrypted with the plugin's
ECDH-negotiated "secrets" shared key. The plugin must have
previously called NegotiateKey(key_id="secrets") before calling
this method.

- **Request:** `dotfilesd.v1.GetSecretRequest`
- **Response:** `dotfilesd.v1.GetSecretResponse`


## Messages

### GetSecretRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `plugin_name` | string | The plugin requesting the secret. Only secrets assigned to this plugin in secrets.yaml will be returned. |
| `key` | string | Key name within the plugin's secret block (e.g. "api_token"). |

### GetSecretResponse

| Field | Type | Description |
|-------|------|-------------|
| `encrypted_value` | bytes | AES-256-GCM encrypted value (nonce || ciphertext). Encrypted using the "secrets" ECDH-negotiated shared key. |

