package sshx

import (
	"os"
	"regexp"
)

var keyPathVariable = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_]*)%|\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// ExpandKeyPath resolves a key's portable path on this computer. The
// saved path stays unchanged, so each computer uses its own environment.
// Unknown variables stay literal rather than silently becoming empty.
func ExpandKeyPath(path string) string {
	return ResolveKeyPath(path, "")
}

// ResolveKeyPath also resolves SSH_KEYS using the app's local directory,
// when configured, before consulting this computer's environment.
func ResolveKeyPath(path, keyDir string) string {
	path = keyPathVariable.ReplaceAllStringFunc(path, func(variable string) string {
		parts := keyPathVariable.FindStringSubmatch(variable)
		for _, name := range parts[1:] {
			if name != "" {
				if name == "SSH_KEYS" && keyDir != "" {
					return ExpandKeyPath(keyDir)
				}
				if value, ok := os.LookupEnv(name); ok {
					return value
				}
				break
			}
		}
		return variable
	})
	return ExpandHome(path)
}
