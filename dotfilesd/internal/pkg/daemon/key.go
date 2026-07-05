package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// keyServer implements KeyServiceHandler for ECDH key negotiation.
type keyServer struct {
	sessions *SessionStore
}

// NegotiateKey performs an ephemeral X25519 ECDH key exchange.
// The client sends its public key and a key_id; the daemon generates its
// own ephemeral keypair, derives the shared secret, stores it for the
// session+key_id, and returns its public key.
func (s *keyServer) NegotiateKey(ctx context.Context, req *connect.Request[dotfilesdv1.NegotiateKeyRequest]) (*connect.Response[dotfilesdv1.NegotiateKeyResponse], error) {
	r := req.Msg
	session := s.sessions.ResolveSession(r.GetSession())

	clientPub := r.GetClientPublicKey()
	if len(clientPub) != 32 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("client_public_key must be 32 bytes, got %d", len(clientPub)))
	}

	keyID := r.GetKeyId()
	if keyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("key_id is required"))
	}

	var ttlDuration time.Duration
	if ttl := r.GetTtlSeconds(); ttl > 0 {
		ttlDuration = time.Duration(ttl) * time.Second
	}

	serverPub, err := session.NegotiateSharedKey(keyID, clientPub, ttlDuration)
	if err != nil {
		slog.Error("key negotiation failed", "session_id", session.id, "key_id", keyID, "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("key negotiation: %w", err))
	}

	slog.Log(ctx, levelTrace, "KeyService.NegotiateKey done", "session_id", session.id, "key_id", keyID)
	return connect.NewResponse(&dotfilesdv1.NegotiateKeyResponse{
		ServerPublicKey: serverPub,
	}), nil
}
