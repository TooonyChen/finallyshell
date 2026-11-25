package localterm

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

const maxMessage = 64 << 10

// Control messages carry configuration and exit status only. Terminal I/O stays
// in Windows' existing console; it never travels over these pipes.
type message struct {
	Kind       string   `json:"kind"`
	Args       []string `json:"args,omitempty"`
	Dir        string   `json:"dir,omitempty"`
	ConsolePID uint32   `json:"consolePid,omitempty"`
	Code       uint32   `json:"code,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type channel struct {
	io.ReadWriteCloser
	writeMu sync.Mutex
}

func (c *channel) send(m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(b) > maxMessage {
		return errors.New("terminal control message too large")
	}
	var h [4]byte
	binary.LittleEndian.PutUint32(h[:], uint32(len(b)))
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := writeAll(c, h[:]); err != nil {
		return err
	}
	return writeAll(c, b)
}

func (c *channel) receive() (message, error) {
	var h [4]byte
	if _, err := io.ReadFull(c, h[:]); err != nil {
		return message{}, err
	}
	n := binary.LittleEndian.Uint32(h[:])
	if n == 0 || n > maxMessage {
		return message{}, errors.New("invalid terminal control message size")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(c, b); err != nil {
		return message{}, err
	}
	var m message
	err := json.Unmarshal(b, &m)
	return m, err
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n < 0 || n > len(b) {
			return io.ErrShortWrite
		}
		b = b[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
