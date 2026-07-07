package cli

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
	"golang.org/x/term"
)

// execCryptoKeyCache stores the derived shared secrets for encrypting
// sensitive data sent to the daemon (e.g. sudo passwords). Keys are
// identified by key_id (e.g. "sudo") and are derived via the ECDH
// key negotiation in Connect().
var execCryptoKeyCache struct {
	mu   sync.Mutex
	keys map[string][]byte // key_id → 32-byte AES-256-GCM key
}

func init() {
	execCryptoKeyCache.keys = make(map[string][]byte)
}

// getCryptoKey returns a copy of the cached derived key for the given key_id.
// The caller MUST zero the returned slice after use.
func getCryptoKey(keyID string) []byte {
	execCryptoKeyCache.mu.Lock()
	defer execCryptoKeyCache.mu.Unlock()
	key, ok := execCryptoKeyCache.keys[keyID]
	if !ok || len(key) != 32 {
		return nil
	}
	k := make([]byte, 32)
	copy(k, key)
	return k
}

// setCryptoKey stores the derived key for the given key_id, zeroing any old key.
func setCryptoKey(keyID string, key []byte) {
	execCryptoKeyCache.mu.Lock()
	defer execCryptoKeyCache.mu.Unlock()
	if old, ok := execCryptoKeyCache.keys[keyID]; ok {
		zeroBytes(old)
	}
	k := make([]byte, 32)
	copy(k, key)
	execCryptoKeyCache.keys[keyID] = k
}

// encryptForDaemon encrypts plaintext with the shared key identified by key_id.
// Returns (ciphertext, key_id) where ciphertext is nonce||ciphertext.
func encryptForDaemon(plaintext []byte, keyID string) ([]byte, string, error) {
	key := getCryptoKey(keyID)
	if key == nil {
		return plaintext, "", nil // fallback: no key negotiated
	}
	defer zeroBytes(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, "", fmt.Errorf("aes new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", fmt.Errorf("aes gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, "", fmt.Errorf("nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, keyID, nil
}

// detectCapabilities returns a set of capability flags describing what
// interactive authentication methods the client environment supports.
// These are sent to the daemon as session variables so it can choose the
// right sudo strategy (terminal prompt vs graphical pkexec vs error).
func detectCapabilities() map[string]string {
	caps := make(map[string]string)

	if clientCaps.hasElicitation {
		caps["_cap_elicitation"] = "true"
	}

	// Advertise MCP client identity so the daemon can adapt behaviour
	// (e.g. prefer pkexec over elicitation for VS Code which doesn't
	// support password fields in its elicitation forms).
	if clientCaps.clientName != "" {
		caps["_cap_client_name"] = clientCaps.clientName
		caps["_cap_client_version"] = clientCaps.clientVersion
	}
	if clientCaps.hasMcpApps {
		caps["_cap_mcp_apps"] = "true"
	}

	// Terminal capability: can we interact with the user via /dev/tty?
	if f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		f.Close()
		caps["_cap_terminal"] = "true"
	}

	// Graphical capability: is a desktop session available?
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		caps["_cap_graphical"] = "true"
	}

	return caps
}

type Clients struct {
	Sys       dotfilesdv1connect.SystemServiceClient
	Dot       dotfilesdv1connect.DotfilesServiceClient
	Exec      dotfilesdv1connect.ExecServiceClient
	Key       dotfilesdv1connect.KeyServiceClient
	Cfg       dotfilesdv1connect.ConfigServiceClient
	Session   dotfilesdv1connect.SessionServiceClient
	Script    dotfilesdv1connect.ScriptServiceClient
	Registry  dotfilesdv1connect.PluginRegistryServiceClient
	Executor  dotfilesdv1connect.PluginExecutorServiceClient
	DiagQuery dotfilesdv1connect.DiagnosticsQueryServiceClient
	DiagPost  dotfilesdv1connect.DiagnosticsPostServiceClient
	Feedback  *FeedbackServer
	SessionID string
	ClientID  string

	// Client context for diagnostics enrichment.
	ClientType  string // "cli" or "mcp"
	CommandPath string // full command path being executed (e.g. "system diag")
	PWD         string // working directory where CLI was invoked
	AgentID     string // MCP agent identity (client name from initialize)

	// ownSession tracks whether this instance created the session (as opposed to
	// inheriting it from the DOTFILESD_SESSION environment). Only own sessions
	// should be finalized in Close() — inherited ones belong to the parent shell.
	ownSession bool

	mu        sync.Mutex
	connected bool
}

func NewClients(port string) *Clients {
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", port)
	return &Clients{
		Sys:       dotfilesdv1connect.NewSystemServiceClient(http.DefaultClient, baseURL),
		Dot:       dotfilesdv1connect.NewDotfilesServiceClient(http.DefaultClient, baseURL),
		Exec:      dotfilesdv1connect.NewExecServiceClient(http.DefaultClient, baseURL),
		Key:       dotfilesdv1connect.NewKeyServiceClient(http.DefaultClient, baseURL),
		Cfg:       dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, baseURL),
		Session:   dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, baseURL),
		Script:    dotfilesdv1connect.NewScriptServiceClient(http.DefaultClient, baseURL),
		Registry:  dotfilesdv1connect.NewPluginRegistryServiceClient(http.DefaultClient, baseURL),
		Executor:  dotfilesdv1connect.NewPluginExecutorServiceClient(http.DefaultClient, baseURL),
		DiagQuery: dotfilesdv1connect.NewDiagnosticsQueryServiceClient(http.DefaultClient, baseURL),
		DiagPost:  dotfilesdv1connect.NewDiagnosticsPostServiceClient(http.DefaultClient, baseURL),
	}
}

func (c *Clients) Connect(ctx context.Context) error {
	c.mu.Lock()
	already := c.connected
	c.mu.Unlock()

	if already {
		// Health check: verify the daemon is reachable AND our session is
		// still valid (survived a restart).
		if _, err := c.Sys.Ping(ctx, connect.NewRequest(&dotfilesdv1.PingRequest{})); err != nil {
			slog.Debug("daemon unreachable, reconnecting", "error", err)
			c.mu.Lock()
			c.connected = false
			if c.Feedback != nil {
				c.Feedback.Close()
				c.Feedback = nil
			}
			c.mu.Unlock()
		} else if c.SessionID != "" {
			// Daemon is up — check if our session still exists
			// (GetSession returns empty session if not found, no error).
			req := connect.NewRequest(&dotfilesdv1.GetSessionRequest{
				Session: &dotfilesdv1.Session{Id: c.SessionID},
			})
			resp, err := c.Session.GetSession(ctx, req)
			stale := err != nil || resp.Msg.GetSession().GetId() != c.SessionID
			if stale {
				slog.Debug("session stale, clearing for reconnect", "session_id", c.SessionID)
				c.mu.Lock()
				c.connected = false
				c.SessionID = "" // clear so Connect creates a fresh one
				if c.Feedback != nil {
					c.Feedback.Close()
					c.Feedback = nil
				}
				c.mu.Unlock()
			} else {
				return nil
			}
		} else {
			return nil
		}
	}

	c.mu.Lock()
	if c.connected {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	slog.Debug("client connecting to daemon")

	fb, err := NewFeedbackServer()
	if err != nil {
		return fmt.Errorf("start feedback server: %w", err)
	}
	fb.SetInputHandler(func(ctx context.Context, req *dotfilesdv1.InputRequest) (string, error) {
		prompt := req.Prompt
		if req.Default != "" {
			prompt = fmt.Sprintf("%s [%s]", prompt, req.Default)
		}
		fmt.Fprint(os.Stderr, prompt, " ")

		if req.Sensitive {
			fd := int(os.Stdin.Fd())
			if term.IsTerminal(fd) {
				raw, err := term.ReadPassword(fd)
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return "", err
				}
				val := string(raw)
				zeroBytes(raw)
				if val == "" {
					return req.Default, nil
				}
				return val, nil
			}
			// Not a terminal — fall back to plain line read.
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil {
				return "", err
			}
			line = strings.TrimRight(line, "\n\r")
			if line == "" {
				return req.Default, nil
			}
			return line, nil
		}

		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimRight(line, "\n\r")
		if line == "" {
			return req.Default, nil
		}
		return line, nil
	})
	fb.SetConfirmHandler(func(ctx context.Context, req *dotfilesdv1.ConfirmRequest) (bool, error) {
		suffix := "[y/N]"
		defaultVal := false
		if req.DefaultConfirm {
			suffix = "[Y/n]"
			defaultVal = true
		}
		fmt.Fprint(os.Stderr, req.Message, " ", suffix, " ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return false, err
		}
		line = strings.TrimRight(line, "\n\r")
		switch strings.ToLower(line) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			return defaultVal, nil
		}
	})
	fb.SetChooseHandler(func(ctx context.Context, req *dotfilesdv1.ChooseRequest) (int, string, error) {
		fmt.Fprintln(os.Stderr, req.Prompt)
		for i, opt := range req.Options {
			mark := " "
			if int(req.DefaultIndex) == i {
				mark = ">"
			}
			fmt.Fprintf(os.Stderr, "  %s %d. %s\n", mark, i+1, opt)
		}
		fmt.Fprint(os.Stderr, "Enter number (or empty to cancel): ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return -1, "", err
		}
		line = strings.TrimRight(line, "\n\r")
		if line == "" {
			return -1, "", nil
		}
		var n int
		if _, err := fmt.Sscanf(line, "%d", &n); err != nil || n < 1 || n > len(req.Options) {
			return -1, "", fmt.Errorf("invalid selection: enter 1-%d", len(req.Options))
		}
		idx := n - 1
		return idx, req.Options[idx], nil
	})
	c.Feedback = fb

	// Detect client capabilities and pass them as session variables so the
	// daemon can choose the best sudo authentication strategy.
	// Also pass _diag_parent to link this session to the client node.
	if c.ClientID == "" {
		c.ClientID = fmt.Sprintf("cli_%x_%x", time.Now().UnixNano(), os.Getpid())
	}

	// Apply the global DefaultSessionID (set by --session flag) if no
	// explicit session was configured yet.
	if c.SessionID == "" && DefaultSessionID != "" {
		c.SessionID = DefaultSessionID
	}

	// Generate an ephemeral AES-256 key for encrypting the sudo password
	// cache on the daemon side. The key is sent during Connect and zeroed
	// on session finalize. Only used over localhost, never persisted.
	sudoCacheKey := make([]byte, 32)
	if _, err := rand.Read(sudoCacheKey); err != nil {
		return fmt.Errorf("generate sudo cache key: %w", err)
	}
	defer zeroBytes(sudoCacheKey)

	session := &dotfilesdv1.Session{Id: c.SessionID, Data: map[string]string{
		"_sudo_cache_key": base64.StdEncoding.EncodeToString(sudoCacheKey),
	}}
	caps := detectCapabilities()
	if caps == nil {
		caps = make(map[string]string)
	}
	caps["_diag_parent"] = "client:" + c.ClientID
	session.Variables = caps
	slog.Debug("client capabilities", "caps", caps)

	// Detect client type from execution context if not already set.
	if c.ClientType == "" {
		if mcpBridge != nil {
			c.ClientType = "mcp"
		} else {
			c.ClientType = "cli"
		}
	}

	// Build rich diagnostic attrs for the client node.
	attrs := map[string]string{
		"client_type": c.ClientType,
	}
	if c.CommandPath != "" {
		attrs["command"] = c.CommandPath
	}
	if c.PWD != "" {
		attrs["pwd"] = c.PWD
	}
	if c.AgentID != "" {
		attrs["agent_id"] = c.AgentID
	} else if clientCaps.clientName != "" {
		attrs["agent_id"] = clientCaps.clientName
	}

	// Remember whether we owned the session before calling Connect (empty ID means
	// the daemon will create a fresh one we own). Used in Close() to decide whether
	// to finalize.
	hadSession := c.SessionID != ""

	// Push client_connect diagnostic event.
	if _, err := c.DiagPost.PostEvent(ctx, connect.NewRequest(&dotfilesdv1.DiagEvent{
		Type:        dotfilesdv1.EventType_EVENT_TYPE_LIFECYCLE,
		Resource:    "client:" + c.ClientID,
		Message:     c.ClientID,
		TimestampNs: time.Now().UnixNano(),
		Labels:      map[string]string{"event_type": "client_connect"},
		Attrs:       attrs,
	})); err != nil {
		slog.Debug("diag post client_connect failed", "error", err)
	}

	req := connect.NewRequest(&dotfilesdv1.ConnectRequest{
		CallbackUrl: fb.URL(),
		Session:     session,
	})

	resp, err := c.Session.Connect(ctx, req)
	if err != nil {
		fb.Close()
		return fmt.Errorf("daemon connect: %w", err)
	}

	c.SessionID = resp.Msg.Session.Id
	c.ownSession = !hadSession
	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()

	slog.Debug("client connected", "session_id", c.SessionID, "own_session", c.ownSession, "feedback_url", fb.URL())

	// Negotiate an ephemeral ECDH key for encrypting sudo passwords.
	// This is best-effort — if the daemon doesn't support it (old version),
	// we fall back to the legacy plaintext approach.
	_ = c.negotiateKey(ctx, "sudo")
	return nil
}

// negotiateKey performs an ECDH X25519 key exchange with the daemon for the
// given key_id. The derived shared secret is stored in the local key cache
// and can be used to encrypt data before sending to the daemon.
func (c *Clients) negotiateKey(ctx context.Context, keyID string) error {
	curve := ecdh.X25519()
	clientKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate client ecdh key: %w", err)
	}
	defer zeroBytes(clientKey.Bytes()) // zero private key bytes after use

	clientPublic := clientKey.PublicKey().Bytes()

	resp, err := c.Key.NegotiateKey(ctx, connect.NewRequest(&dotfilesdv1.NegotiateKeyRequest{
		Session:          &dotfilesdv1.Session{Id: c.SessionID},
		KeyId:            keyID,
		ClientPublicKey:  clientPublic,
	}))
	if err != nil {
		return fmt.Errorf("key negotiation: %w", err)
	}

	serverPub, err := curve.NewPublicKey(resp.Msg.ServerPublicKey)
	if err != nil {
		return fmt.Errorf("invalid server public key: %w", err)
	}

	secret, err := clientKey.ECDH(serverPub)
	if err != nil {
		return fmt.Errorf("ecdh: %w", err)
	}

	setCryptoKey(keyID, secret)
	return nil
}

func (c *Clients) Close() {
	if c.ClientID != "" {
		_, err := c.DiagPost.PostEvent(context.Background(), connect.NewRequest(&dotfilesdv1.DiagEvent{
			Type:        dotfilesdv1.EventType_EVENT_TYPE_LIFECYCLE,
			Resource:    "client:" + c.ClientID,
			Message:     c.ClientID,
			TimestampNs: time.Now().UnixNano(),
			Labels:      map[string]string{"event_type": "client_disconnect"},
		}))
		if err != nil {
			slog.Debug("diag post client_disconnect failed", "error", err)
		}

		// Only finalize sessions we created ourselves. Inherited sessions
		// (from DOTFILESD_SESSION env) belong to the parent shell and must
		// not be finalized.
		if c.ownSession && c.SessionID != "" {
			if _, err := c.Session.FinalizeSession(context.Background(), connect.NewRequest(&dotfilesdv1.FinalizeSessionRequest{
				Session: &dotfilesdv1.Session{Id: c.SessionID},
			})); err != nil {
				slog.Debug("session finalize failed", "error", err)
			}
			slog.Debug("client session finalized", "session_id", c.SessionID)
		}
	}
	if c.Feedback != nil {
		c.Feedback.Close()
	}
}
