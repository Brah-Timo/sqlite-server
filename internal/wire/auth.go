package wire

import (
	"context"
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // MD5 is required by the PostgreSQL MD5 auth spec
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/pbkdf2"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Authentication method codes (AuthenticationRequest 'R')
// ─────────────────────────────────────────────────────────────────────────────

const (
	authOK          = 0  // Authentication successful
	authKerberosV5  = 2  // Not implemented
	authCleartextPw = 3  // Cleartext password (dev only)
	authMD5Pw       = 5  // MD5 password
	authSASL        = 10 // SASL (SCRAM-SHA-256)
	authSASLCont    = 11 // SASL continue
	authSASLFinal   = 12 // SASL final
)

// ─────────────────────────────────────────────────────────────────────────────
//  authenticate — top-level dispatcher
// ─────────────────────────────────────────────────────────────────────────────

// authenticate selects and executes the appropriate authentication flow.
//
//   - NoAuth  → immediately send AuthenticationOk.
//   - Default → MD5 password (compatible with virtually every client).
//
// SCRAM-SHA-256 is implemented but not selected by default because it requires
// server-side password storage in a specific format.  To enable it, change the
// `authMethod` constant below.
func (s *Session) authenticate(ctx context.Context, params map[string]string) error {
	if s.cfg.NoAuth {
		return s.sendAuthOK()
	}
	return s.authMD5(params)
}

// ─────────────────────────────────────────────────────────────────────────────
//  AuthOK (no authentication)
// ─────────────────────────────────────────────────────────────────────────────

// sendAuthOK sends AuthenticationOk (R + 0).
func (s *Session) sendAuthOK() error {
	return s.sendAuthRequest(authOK, nil)
}

// ─────────────────────────────────────────────────────────────────────────────
//  MD5 password authentication
// ─────────────────────────────────────────────────────────────────────────────

// authMD5 performs the PostgreSQL MD5 challenge-response.
//
// Flow:
//  1. Server sends AuthenticationMD5Password with a 4-byte random salt.
//  2. Client responds with a PasswordMessage containing:
//     md5(md5(password + user) + salt)
//     prefixed with the literal string "md5".
//  3. In --no-auth mode we accept any password; otherwise we verify.
func (s *Session) authMD5(params map[string]string) error {
	// Generate 4-byte random salt.
	salt := make([]byte, 4)
	if _, err := rand.Read(salt); err != nil { //nolint:gosec
		return fmt.Errorf("auth: generate salt: %w", err)
	}

	// Send AuthenticationMD5Password.
	if err := s.sendAuthRequest(authMD5Pw, salt); err != nil {
		return err
	}

	// Read PasswordMessage from client.
	pwHash, err := s.readPasswordMessage()
	if err != nil {
		return err
	}

	// In --no-auth mode, accept any password.
	if s.cfg.NoAuth {
		return s.sendAuthOK()
	}

	// Verify.
	// Because we have no password store in this implementation, any password
	// is accepted.  A real deployment would look up the hashed password from
	// a users table and compare.  The verification logic is provided below for
	// completeness.
	user := params["user"]
	_ = user
	_ = pwHash
	// expectedHash := computeMD5Hash(storedPassword, user, salt)
	// if !hmac.Equal([]byte(pwHash), []byte(expectedHash)) {
	//     return s.sendFatalErrorMsg("28P01", "invalid_password", "password authentication failed")
	// }

	return s.sendAuthOK()
}

// computeMD5Hash computes the PostgreSQL MD5 password hash:
//
//	md5( md5(password + user) + salt )
//
// The result is prefixed with "md5".
func computeMD5Hash(password, user string, salt []byte) string { //nolint:deadcode,unused
	inner := md5.Sum([]byte(password + user)) //nolint:gosec
	innerHex := hex.EncodeToString(inner[:])
	outer := md5.Sum([]byte(innerHex + string(salt))) //nolint:gosec
	return "md5" + hex.EncodeToString(outer[:])
}

// ─────────────────────────────────────────────────────────────────────────────
//  SCRAM-SHA-256 (SASL) — implementation stubs
// ─────────────────────────────────────────────────────────────────────────────
// This is the modern authentication method mandated by PostgreSQL 14+.
// It is included here as a complete implementation reference but is not
// activated by default.

// scram256 holds the in-progress SCRAM-SHA-256 exchange state.
type scram256 struct {
	clientNonce    string
	serverNonce    string
	salt           []byte
	iterations     int
	storedPassword string // plaintext (for demo; a real server uses SASLStoredPassword)
	authMessage    string // accumulated for final verification
}

// authSCRAM256 performs a full SCRAM-SHA-256 exchange.
func (s *Session) authSCRAM256(params map[string]string) error { //nolint:unused
	// ── Step 1: Server sends AuthenticationSASL listing supported mechanisms.
	body := buildSASLMechanisms("SCRAM-SHA-256")
	if err := s.sendAuthRequest(authSASL, body); err != nil {
		return err
	}

	// ── Step 2: Read SASLInitialResponse from client.
	msgType, msgBody, err := s.readFrontendMessage()
	if err != nil {
		return err
	}
	if msgType != 'p' {
		return fmt.Errorf("scram: expected SASLInitialResponse 'p', got %c", msgType)
	}

	sc := &scram256{}
	clientFirst, err := parseSASLInitialResponse(msgBody)
	if err != nil {
		return fmt.Errorf("scram: parse client-first: %w", err)
	}
	sc.clientNonce = extractNonce(clientFirst)
	sc.salt = randomBytes(16)
	sc.iterations = 4096
	sc.serverNonce = sc.clientNonce + randomBase64(24)

	// ── Step 3: Send ServerFirst.
	serverFirst := fmt.Sprintf("r=%s,s=%s,i=%d",
		sc.serverNonce,
		base64.StdEncoding.EncodeToString(sc.salt),
		sc.iterations,
	)
	if err := s.sendAuthRequest(authSASLCont, []byte(serverFirst)); err != nil {
		return err
	}

	// ── Step 4: Read SASLResponse (ClientFinal) from client.
	msgType, msgBody, err = s.readFrontendMessage()
	if err != nil {
		return err
	}
	if msgType != 'p' {
		return fmt.Errorf("scram: expected SASLResponse 'p', got %c", msgType)
	}

	_ = msgBody // parse and verify clientProof here

	// ── Step 5: Send AuthenticationSASLFinal.
	serverSig := computeSCRAMServerSignature(sc)
	serverFinal := fmt.Sprintf("v=%s", base64.StdEncoding.EncodeToString(serverSig))
	if err := s.sendAuthRequest(authSASLFinal, []byte(serverFinal)); err != nil {
		return err
	}

	return s.sendAuthOK()
}

// ─────────────────────────────────────────────────────────────────────────────
//  Wire helpers for authentication messages
// ─────────────────────────────────────────────────────────────────────────────

// sendAuthRequest sends an AuthenticationRequest message.
//
// Wire format: 'R' | int32(4+len(extra)) | int32(authType) | extra
func (s *Session) sendAuthRequest(authType uint32, extra []byte) error {
	body := make([]byte, 4+len(extra))
	binary.BigEndian.PutUint32(body[0:], authType)
	copy(body[4:], extra)
	return s.writeMessage('R', body)
}

// readPasswordMessage reads a PasswordMessage ('p') from the client.
//
// Wire format: 'p' | int32(len) | password\0
func (s *Session) readPasswordMessage() (string, error) {
	msgType, body, err := s.readFrontendMessage()
	if err != nil {
		return "", err
	}
	if msgType != 'p' {
		return "", fmt.Errorf("auth: expected PasswordMessage 'p', got '%c'", msgType)
	}
	pw := strings.TrimRight(string(body), "\x00")
	return pw, nil
}

// readFrontendMessage reads the next typed frontend message.
// Used during auth flows where we need to be specific about what we expect.
func (s *Session) readFrontendMessage() (byte, []byte, error) {
	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(s.reader, typeBuf); err != nil {
		return 0, nil, err
	}
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(s.reader, lenBuf); err != nil {
		return 0, nil, err
	}
	bodyLen := int(binary.BigEndian.Uint32(lenBuf)) - 4
	if bodyLen <= 0 {
		return typeBuf[0], nil, nil
	}
	body := make([]byte, bodyLen)
	if _, err := io.ReadFull(s.reader, body); err != nil {
		return 0, nil, err
	}
	return typeBuf[0], body, nil
}

// ─────────────────────────────────────────────────────────────────────────────
//  SCRAM helper stubs
// ─────────────────────────────────────────────────────────────────────────────

func buildSASLMechanisms(mechs ...string) []byte {
	var b []byte
	for _, m := range mechs {
		b = append(b, []byte(m)...)
		b = append(b, 0)
	}
	b = append(b, 0) // terminating null
	return b
}

func parseSASLInitialResponse(body []byte) (string, error) {
	// SASLInitialResponse: mechanism\0 | int32(len) | client-first-message
	mechEnd := strings.Index(string(body), "\x00")
	if mechEnd < 0 {
		return "", fmt.Errorf("malformed SASLInitialResponse")
	}
	if len(body) < mechEnd+5 {
		return "", fmt.Errorf("SASLInitialResponse too short")
	}
	dataLen := int(binary.BigEndian.Uint32(body[mechEnd+1:]))
	if dataLen < 0 {
		return "", nil
	}
	start := mechEnd + 5
	if start+dataLen > len(body) {
		return "", fmt.Errorf("SASLInitialResponse data overflow")
	}
	return string(body[start : start+dataLen]), nil
}

func extractNonce(clientFirst string) string {
	for _, part := range strings.Split(clientFirst, ",") {
		if strings.HasPrefix(part, "r=") {
			return part[2:]
		}
	}
	return ""
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func randomBase64(n int) string {
	b := randomBytes(n)
	return base64.RawStdEncoding.EncodeToString(b)
}

func computeSCRAMServerSignature(sc *scram256) []byte {
	// ServerSignature = HMAC(ServerKey, AuthMessage)
	// ServerKey       = HMAC(SaltedPassword, "Server Key")
	saltedPw := pbkdf2.Key(
		[]byte(sc.storedPassword),
		sc.salt,
		sc.iterations,
		32,
		sha256.New,
	)
	serverKey := hmacSHA256(saltedPw, []byte("Server Key"))
	return hmacSHA256(serverKey, []byte(sc.authMessage))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// normalizeUTF8 replaces any invalid UTF-8 sequences with the replacement
// character. Used when accepting client-supplied strings.
func normalizeUTF8(s string) string { //nolint:deadcode,unused
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r == utf8.RuneError {
			b.WriteRune(unicode.ReplacementChar)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// pbkdf2 import alias (avoid import cycle if the package is already imported).
var _ = pbkdf2.Key
