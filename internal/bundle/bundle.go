// Package bundle is the portable on-disk format for `envault export`/`import`:
// a self-contained JSON envelope carrying one or more projects, their stages,
// files and savedAt metadata. The envelope is optionally encrypted with
// AES-256-GCM under a key derived from a password via argon2id; a plaintext
// header lets import detect encryption without the password.
package bundle

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/argon2"
)

const formatVersion = 1

// argon2id parameters (interactive profile from the RFC 9106 recommendations).
const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
	argonKeyLen  = 32 // AES-256
	saltLen      = 16
)

// File mirrors vault.File but with an explicit JSON shape; Content rides as
// base64 (encoding/json's default for []byte).
type File struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
}

// Stage is one (stage, savedAt, files) triple under a project.
type Stage struct {
	Name    string    `json:"name"`
	SavedAt time.Time `json:"savedAt"`
	Files   []File    `json:"files"`
}

// Project groups the stages exported for a single project.
type Project struct {
	Name   string  `json:"name"`
	Stages []Stage `json:"stages"`
}

// Payload is the decrypted bundle content.
type Payload struct {
	Projects []Project `json:"projects"`
}

type kdfParams struct {
	Algo    string `json:"algo"`
	Salt    []byte `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
	KeyLen  uint32 `json:"keyLen"`
}

// envelope is the wire format. Exactly one of Payload / Ciphertext is set,
// keyed by Encrypted.
type envelope struct {
	Format     int        `json:"envault_bundle"`
	Encrypted  bool       `json:"encrypted"`
	KDF        *kdfParams `json:"kdf,omitempty"`
	Cipher     string     `json:"cipher,omitempty"`
	Nonce      []byte     `json:"nonce,omitempty"`
	Ciphertext []byte     `json:"ciphertext,omitempty"`
	Payload    *Payload   `json:"payload,omitempty"`
}

// Bundle is a parsed envelope: the header is readable, the payload may still be
// encrypted (call Decode).
type Bundle struct {
	env envelope
}

// Build serializes payload into a bundle. When encrypt is true the payload is
// sealed with AES-256-GCM under an argon2id key derived from password; password
// must be non-empty in that case. With encrypt false the payload travels in
// clear and password is ignored.
func Build(payload Payload, password string, encrypt bool) ([]byte, error) {
	env := envelope{Format: formatVersion, Encrypted: encrypt}
	if !encrypt {
		p := payload
		env.Payload = &p
		return marshal(env)
	}

	if password == "" {
		return nil, errors.New("password required to encrypt bundle")
	}

	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	env.KDF = &kdfParams{
		Algo: "argon2id", Salt: salt, Time: argonTime,
		Memory: argonMemory, Threads: argonThreads, KeyLen: argonKeyLen,
	}
	env.Cipher = "AES-256-GCM"
	env.Nonce = nonce
	env.Ciphertext = gcm.Seal(nil, nonce, plain, nil)
	return marshal(env)
}

func marshal(env envelope) ([]byte, error) {
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Parse reads a bundle's envelope without decrypting it.
func Parse(data []byte) (*Bundle, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("not a valid envault bundle: %w", err)
	}
	if env.Format != formatVersion {
		return nil, fmt.Errorf("unsupported bundle format version %d", env.Format)
	}
	return &Bundle{env: env}, nil
}

// IsEncrypted reports whether decoding this bundle needs a password.
func (b *Bundle) IsEncrypted() bool { return b.env.Encrypted }

// Decode returns the payload, decrypting with password when the bundle is
// encrypted. A wrong password or tampered bundle yields an error and no
// partial payload. password is ignored for a plaintext bundle.
func (b *Bundle) Decode(password string) (*Payload, error) {
	if !b.env.Encrypted {
		if b.env.Payload == nil {
			return nil, errors.New("bundle has no payload")
		}
		return b.env.Payload, nil
	}

	if b.env.KDF == nil || b.env.KDF.Algo != "argon2id" {
		return nil, errors.New("bundle is missing or has an unsupported key derivation")
	}
	if b.env.Cipher != "AES-256-GCM" {
		return nil, fmt.Errorf("unsupported bundle cipher %q", b.env.Cipher)
	}
	if password == "" {
		return nil, errors.New("password required to decrypt bundle")
	}

	k := b.env.KDF
	key := argon2.IDKey([]byte(password), k.Salt, k.Time, k.Memory, k.Threads, k.KeyLen)
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, b.env.Nonce, b.env.Ciphertext, nil)
	if err != nil {
		return nil, errors.New("wrong password or corrupt bundle")
	}

	var payload Payload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, fmt.Errorf("corrupt bundle payload: %w", err)
	}
	return &payload, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
