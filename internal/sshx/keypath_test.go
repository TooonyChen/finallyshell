package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"finallyshell/internal/config"

	"golang.org/x/crypto/ssh"
)

func TestExpandKeyPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FINALLYSHELL_TEST_KEYS", dir)
	t.Setenv("FINALLYSHELL_TEST_LITERAL", "$UNEXPANDED")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, want string }{
		{"%FINALLYSHELL_TEST_KEYS%/key.pem", filepath.ToSlash(dir) + "/key.pem"},
		{"$FINALLYSHELL_TEST_KEYS/key.pem", filepath.ToSlash(dir) + "/key.pem"},
		{"${FINALLYSHELL_TEST_KEYS}/key.pem", filepath.ToSlash(dir) + "/key.pem"},
		{"~/key.pem", filepath.Join(home, "key.pem")},
		{"~", home},
		{"~someone/key.pem", "~someone/key.pem"},
		{"%FINALLYSHELL_UNDEFINED_KEY_PATH%/key.pem", "%FINALLYSHELL_UNDEFINED_KEY_PATH%/key.pem"},
		{"${FINALLYSHELL_UNDEFINED_KEY_PATH}/key.pem", "${FINALLYSHELL_UNDEFINED_KEY_PATH}/key.pem"},
		{"$FINALLYSHELL_TEST_LITERAL/key.pem", "$UNEXPANDED/key.pem"},
		{"/keys/$100.pem", "/keys/$100.pem"},
		{`%FINALLYSHELL_TEST_KEYS%\key.pem`, dir + `\key.pem`},
	} {
		// Environment values retain their native separators.
		if got := ExpandKeyPath(test.path); filepath.ToSlash(got) != filepath.ToSlash(test.want) {
			t.Errorf("ExpandKeyPath(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestAuthUsesPortableKeyPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FINALLYSHELL_TEST_KEYS", dir)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"%FINALLYSHELL_TEST_KEYS%/key.pem", "$FINALLYSHELL_TEST_KEYS/key.pem", "${FINALLYSHELL_TEST_KEYS}/key.pem"} {
		h := &config.Host{Auth: config.AuthKey, KeyPath: path}
		methods, _, err := authMethods(h, Options{})
		if err != nil || len(methods) != 1 || h.KeyPath != path {
			t.Fatalf("auth with %q: methods %d, error %v, stored path %q", path, len(methods), err, h.KeyPath)
		}
	}
	t.Setenv("SSH_KEYS", filepath.Join(dir, "missing-environment-folder"))
	for _, path := range []string{"%SSH_KEYS%/key.pem", "$SSH_KEYS/key.pem", "${SSH_KEYS}/key.pem"} {
		h := &config.Host{Auth: config.AuthKey, KeyPath: path}
		methods, _, err := authMethods(h, Options{KeyDir: dir})
		if err != nil || len(methods) != 1 || h.KeyPath != path {
			t.Fatalf("auth with configured directory %q: methods %d, error %v", path, len(methods), err)
		}
	}
}
