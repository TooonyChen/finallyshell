package main

import (
	"errors"

	"github.com/egoist/mygo"
)

// secrets is the system's credential store for the hosts' passwords. Its
// service is the app's name, not its identifier, which MyGo would take:
// `mygo dev` (whose identifier ends in ".dev"), `go run` and the
// installed app share the secrets, as they share hosts.json, whose host
// IDs the keys hold.
var secrets = mygo.Secrets.Service("finallyshell")

// SecretGet and the others block while the system asks the user; the app
// calls them on a goroutine, where MyGo calls the store directly.
func (p *platform) SecretGet(key string) (string, bool, error) {
	v, err := secrets.Get(key)
	switch {
	case errors.Is(err, mygo.ErrSecretNotFound):
		return "", false, nil
	case err != nil:
		return "", false, err
	}
	return string(v), true, nil
}

func (p *platform) SecretSet(key, value string) error { return secrets.Set(key, []byte(value)) }
func (p *platform) SecretDelete(key string) error     { return secrets.Delete(key) }
