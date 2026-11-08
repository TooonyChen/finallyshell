package cloudsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSealOpen(t *testing.T) {
	env, err := Seal("hunter2", []byte(`{"secret":"value"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(env, []byte("secret")) || bytes.Contains(env, []byte("value")) {
		t.Fatalf("plaintext shows in the envelope: %s", env)
	}
	plain, err := Open("hunter2", env)
	if err != nil || string(plain) != `{"secret":"value"}` {
		t.Fatalf("Open = %q, %v", plain, err)
	}
	if _, err := Open("hunter3", env); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}
}

func TestSealKeepsSaltNewNonce(t *testing.T) {
	a, _ := Seal("pw", []byte("one"), nil)
	b, _ := Seal("pw", []byte("two"), a)
	var ea, eb Envelope
	json.Unmarshal(a, &ea)
	json.Unmarshal(b, &eb)
	if !bytes.Equal(ea.Salt, eb.Salt) {
		t.Error("the salt changed")
	}
	if bytes.Equal(ea.Nonce, eb.Nonce) {
		t.Error("the nonce was used again")
	}
	if ea.Time != argonTime || ea.Memory != argonMemory || ea.Lanes != argonThreads || ea.KDF != "argon2id" {
		t.Errorf("parameters: %+v", ea)
	}
	c, _ := Seal("pw", []byte("three"), []byte("not an envelope"))
	var ec Envelope
	json.Unmarshal(c, &ec)
	if bytes.Equal(ea.Salt, ec.Salt) {
		t.Error("a new file kept another's salt")
	}
}

func TestOpenTampered(t *testing.T) {
	env, _ := Seal("pw", []byte("data"), nil)
	var e Envelope
	json.Unmarshal(env, &e)
	e.Data[0] ^= 1
	bad, _ := json.Marshal(e)
	if _, err := Open("pw", bad); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("tampered data: %v", err)
	}
}

func TestOpenRejects(t *testing.T) {
	env, _ := Seal("pw", []byte("data"), nil)
	var e Envelope
	json.Unmarshal(env, &e)
	newer := e
	newer.Format = EnvelopeFormat + 1
	huge := e
	huge.Memory = 64 * 1024 * 1024 // 64 GiB
	for name, data := range map[string][]byte{
		"empty":  nil,
		"json":   []byte(`{"hello":1}`),
		"text":   []byte("hello"),
		"newer":  must(json.Marshal(newer)),
		"memory": must(json.Marshal(huge)),
	} {
		_, err := Open("pw", data)
		want := ErrNotEnvelope
		if name == "newer" {
			want = ErrNewerFormat
		}
		if !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", name, err, want)
		}
	}
	if !strings.Contains(string(env), `"kdf": "argon2id"`) {
		t.Errorf("envelope: %s", env)
	}
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
