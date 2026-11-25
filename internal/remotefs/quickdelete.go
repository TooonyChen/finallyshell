package remotefs

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// RemoveWithShell removes names in dir with one SSH exec command. It
// returns the paths already gone even if rm fails partway through.
func RemoveWithShell(ctx context.Context, client *ssh.Client, sc *sftp.Client, dir string, names []string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("no SSH connection to run rm over")
	}
	absolute := dir
	if dir != "" && !path.IsAbs(dir) {
		var err error
		absolute, err = sc.RealPath(dir)
		if err != nil {
			return nil, err
		}
	}
	command, err := removeCommand(absolute, names)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stderr := &tailBuffer{max: 4 << 10}
	sess.Stderr = stderr
	stop := context.AfterFunc(ctx, func() {
		sess.Signal(ssh.SIGKILL)
		sess.Close()
	})
	err = sess.Run(command)
	stop()
	if ctx.Err() != nil {
		err = ctx.Err()
	} else if err != nil {
		if message := stderr.lines(3); message != "" {
			err = fmt.Errorf("%s (%w)", message, err)
		} else {
			err = fmt.Errorf("rm: %w", err)
		}
	}

	var remaining map[string]bool
	if err != nil {
		// rm can remove some operands before reporting an error. Use
		// one SFTP listing to find them so a retry omits those entries.
		entries, listErr := sc.ReadDir(dir)
		if listErr != nil {
			return nil, err
		}
		remaining = make(map[string]bool, len(entries))
		for _, entry := range entries {
			remaining[entry.Name()] = true
		}
	}
	var gone []string
	for _, name := range names {
		if !remaining[name] {
			gone = append(gone, path.Join(dir, name))
		}
	}
	return gone, err
}

// removeCommand takes an absolute directory and literal leaf names.
func removeCommand(dir string, names []string) (string, error) {
	if !path.IsAbs(dir) || strings.ContainsRune(dir, 0) || len(names) == 0 {
		return "", ErrBadName
	}
	var script strings.Builder
	script.WriteString("exec rm -rf --")
	for _, name := range names {
		if !ValidName(name) {
			return "", ErrBadName
		}
		script.WriteByte(' ')
		script.WriteString(ShellQuote(path.Join(dir, name)))
	}
	return "sh -c " + ShellQuote(script.String()), nil
}
