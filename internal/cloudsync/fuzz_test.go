package cloudsync

import (
	"encoding/json"
	"testing"
)

// FuzzOpen feeds arbitrary bytes to Open, which reads a file another
// machine wrote: whatever a hostile sync file can do, it does here first.
//
// The seed corpus covers the shapes that reach the expensive paths: a real
// envelope, a truncated one, and one whose KDF parameters are out of range
// (it must be refused before Argon2id allocates anything).
func FuzzOpen(f *testing.F) {
	valid, err := Seal("pw", []byte(`{"hosts":[]}`), nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{"app":"finallyshell","format":1}`))
	f.Add([]byte(`{"app":"finallyshell","format":1,"kdf":"argon2id","cipher":"aes-256-gcm","salt":"AAAAAAAAAAAAAAAAAAAAAA==","t":99,"m":1048576,"p":1}`))
	f.Add([]byte(`{"app":"finallyshell","format":1,"kdf":"argon2id","cipher":"aes-256-gcm","salt":"AAAAAAAAAAAAAAAAAAAAAA==","t":1,"m":999999999,"p":1}`))
	f.Add([]byte(`{"app":"finallyshell","format":99}`))
	f.Add([]byte(`{}`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		plain, err := Open("pw", data)
		if err != nil {
			if plain != nil {
				t.Fatalf("plaintext and an error together")
			}
			return
		}
		// Only an envelope this package would have written opens: the
		// format, the KDF and the parameters are all checked first.
		var e Envelope
		if json.Unmarshal(data, &e) != nil {
			t.Fatalf("opened a file that is not JSON: %q", data)
		}
		if e.App != "finallyshell" || e.Format != EnvelopeFormat {
			t.Fatalf("opened a foreign envelope: app=%q format=%d", e.App, e.Format)
		}
		if e.KDF != "argon2id" || e.Cipher != "aes-256-gcm" || !validParams(&e) {
			t.Fatalf("opened an envelope with parameters we do not accept")
		}
	})
}
