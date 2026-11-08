package cloudsync

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/argon2"
)

// The file on the remote is an Envelope, as JSON: the payload, encrypted
// with AES-256-GCM under a key that Argon2id derives from the sync
// password. Only the format and the KDF's parameters are in the clear;
// the device, the time and everything else are inside the ciphertext.

// EnvelopeFormat is the version of the envelope this build writes.
const EnvelopeFormat = 1

// The Argon2id parameters of new envelopes: RFC 9106's second
// recommended setting (t=3, 64 MiB, p=4). An envelope keeps its own, so
// they can change later.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
)

// Envelope is the remote file.
type Envelope struct {
	App    string `json:"app"`    // always "finallyshell"
	Format int    `json:"format"` // EnvelopeFormat
	KDF    string `json:"kdf"`    // "argon2id"
	Salt   []byte `json:"salt"`
	Time   uint32 `json:"t"`
	Memory uint32 `json:"m"` // KiB
	Lanes  uint8  `json:"p"`
	Cipher string `json:"cipher"` // "aes-256-gcm"
	Nonce  []byte `json:"nonce"`
	Data   []byte `json:"data"`
}

// The errors of opening an envelope.
var (
	// ErrWrongPassword: the data does not decrypt with the password,
	// which is not the one it was encrypted with (or it was changed).
	ErrWrongPassword = errors.New("cloudsync: wrong sync password")
	// ErrNotEnvelope: the file is not one this app wrote.
	ErrNotEnvelope = errors.New("cloudsync: the remote file is not a Finallyshell sync file")
	// ErrNewerFormat: a newer build wrote the file.
	ErrNewerFormat = errors.New("cloudsync: the remote file is from a newer version of Finallyshell")
)

// aad binds the ciphertext to the format, so that a file cannot pass for
// another format's.
var aad = []byte("finallyshell-sync/1")

// keyCache keeps the keys derived this run, as deriving one takes 64 MiB
// and a noticeable fraction of a second.
var keyCache = struct {
	sync.Mutex
	keys map[[32]byte][]byte
}{keys: map[[32]byte][]byte{}}

func deriveKey(password string, e *Envelope) []byte {
	h := sha256.New()
	fmt.Fprintf(h, "%d/%d/%d/%x/", e.Time, e.Memory, e.Lanes, e.Salt)
	h.Write([]byte(password))
	var id [32]byte
	h.Sum(id[:0])
	keyCache.Lock()
	defer keyCache.Unlock()
	if k, ok := keyCache.keys[id]; ok {
		return k
	}
	k := argon2.IDKey([]byte(password), e.Salt, e.Time, e.Memory, e.Lanes, 32)
	keyCache.keys[id] = k
	return k
}

// Seal encrypts plaintext with password. It keeps the salt and KDF
// parameters of prev when prev is an envelope of this format, so that the
// key derived for it is used again; a new salt otherwise. Each seal has a
// new nonce.
func Seal(password string, plaintext []byte, prev []byte) ([]byte, error) {
	e := &Envelope{App: "finallyshell", Format: EnvelopeFormat, KDF: "argon2id", Cipher: "aes-256-gcm"}
	if p, err := parseEnvelope(prev); err == nil && validParams(p) {
		e.Salt, e.Time, e.Memory, e.Lanes = p.Salt, p.Time, p.Memory, p.Lanes
	} else {
		e.Salt = make([]byte, 16)
		rand.Read(e.Salt)
		e.Time, e.Memory, e.Lanes = argonTime, argonMemory, argonThreads
	}
	gcm, err := newGCM(deriveKey(password, e))
	if err != nil {
		return nil, err
	}
	e.Nonce = make([]byte, gcm.NonceSize())
	rand.Read(e.Nonce)
	e.Data = gcm.Seal(nil, e.Nonce, plaintext, aad)
	return json.MarshalIndent(e, "", "  ")
}

// Open decrypts an envelope with password.
func Open(password string, data []byte) ([]byte, error) {
	e, err := parseEnvelope(data)
	if err != nil {
		return nil, err
	}
	if e.Format > EnvelopeFormat {
		return nil, ErrNewerFormat
	}
	if e.KDF != "argon2id" || e.Cipher != "aes-256-gcm" || !validParams(e) {
		return nil, ErrNotEnvelope
	}
	gcm, err := newGCM(deriveKey(password, e))
	if err != nil {
		return nil, err
	}
	if len(e.Nonce) != gcm.NonceSize() {
		return nil, ErrNotEnvelope
	}
	plain, err := gcm.Open(nil, e.Nonce, e.Data, aad)
	if err != nil {
		return nil, ErrWrongPassword
	}
	return plain, nil
}

func parseEnvelope(data []byte) (*Envelope, error) {
	if len(data) == 0 {
		return nil, ErrNotEnvelope
	}
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil || e.App != "finallyshell" || e.Format < 1 {
		return nil, ErrNotEnvelope
	}
	return &e, nil
}

// validParams keeps a file someone else wrote from asking for a key that
// takes gigabytes or minutes to derive.
func validParams(e *Envelope) bool {
	return len(e.Salt) >= 8 && e.Time >= 1 && e.Time <= 10 &&
		e.Memory >= 8*1024 && e.Memory <= 1024*1024 && e.Lanes >= 1 && e.Lanes <= 16
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
