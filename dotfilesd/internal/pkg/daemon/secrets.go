package daemon

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
	"gopkg.in/yaml.v3"
)

// secretsFile is the default path for the plugin secrets YAML file.
const secretsFilePath = "~/.config/dotfilesd/secrets.yaml"

// secretsServer implements SecretsService — serves encrypted secret values to plugins.
type secretsServer struct {
	sessions *SessionStore
	secrets  map[string]map[string]string // plugin_name -> key -> plaintext value
}

// loadSecretsFile reads and parses the plugin secrets YAML file.
// Expected structure:
//
//	zerotier:
//	  api_token: "xxx"
//	some_plugin:
//	  some_key: "yyy"
func loadSecretsFile(path string) (map[string]map[string]string, error) {
	expanded := expandHome(path)
	data, err := os.ReadFile(expanded)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("no secrets file found, continuing", "path", expanded)
			return make(map[string]map[string]string), nil
		}
		return nil, fmt.Errorf("read secrets file: %w", err)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse secrets file: %w", err)
	}

	secrets := make(map[string]map[string]string, len(raw))
	for pluginName, value := range raw {
		pluginSecrets, ok := value.(map[string]interface{})
		if !ok {
			slog.Warn("secrets: skipping non-map entry", "plugin", pluginName)
			continue
		}
		kv := make(map[string]string, len(pluginSecrets))
		for k, v := range pluginSecrets {
			switch val := v.(type) {
			case string:
				kv[k] = val
			case fmt.Stringer:
				kv[k] = val.String()
			default:
				kv[k] = fmt.Sprintf("%v", v)
			}
		}
		secrets[pluginName] = kv
	}

	slog.Info("loaded plugin secrets", "count", len(secrets), "path", expanded)
	return secrets, nil
}

func newSecretsServer(sessions *SessionStore, secrets map[string]map[string]string) *secretsServer {
	return &secretsServer{sessions: sessions, secrets: secrets}
}

// GetSecret returns a secret value encrypted with the plugin's ECDH "secrets" shared key.
// The caller must have previously negotiated a shared key with key_id="secrets".
func (s *secretsServer) GetSecret(ctx context.Context, req *connect.Request[dotfilesdv1.GetSecretRequest]) (*connect.Response[dotfilesdv1.GetSecretResponse], error) {
	r := req.Msg
	session := s.sessions.ResolveSession(r.GetSession())

	pluginName := r.GetPluginName()
	key := r.GetKey()

	if pluginName == "" || key == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("plugin_name and key are required"))
	}

	// Look up the secret value.
	pluginSecrets, ok := s.secrets[pluginName]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("no secrets for plugin %q", pluginName))
	}
	plaintext, ok := pluginSecrets[key]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("secret %q not found for plugin %q", key, pluginName))
	}

	// Get the ECDH-negotiated shared key for "secrets".
	sharedKey, ok := session.GetSharedKey("secrets")
	if !ok || len(sharedKey) != 32 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("no shared key negotiated for key_id=\"secrets\"; plugin must call NegotiateKey first"))
	}
	defer zeroBytes(sharedKey)

	// Encrypt with AES-256-GCM.
	block, err := aes.NewCipher(sharedKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("aes new cipher: %w", err))
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("aes gcm: %w", err))
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(cryptorand.Reader, nonce); err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("nonce: %w", err))
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	return connect.NewResponse(&dotfilesdv1.GetSecretResponse{
		EncryptedValue: ciphertext,
	}), nil
}

// expandHome replaces "~/" with the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}


