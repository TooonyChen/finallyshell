// Package termenc converts a terminal's connection between a server's
// character encoding, such as GBK or Big5, and the UTF-8 the terminal
// reads and writes. libghostty-vt decodes UTF-8 alone, so the bytes are
// converted before they reach it.
package termenc

import (
	"io"
	"sync"
	"unicode/utf8"

	"finallyshell/internal/config"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// Label is how the options name e, as servers' locales spell it.
func Label(e config.Encoding) string {
	switch e {
	case config.EncodingUTF8:
		return "UTF-8"
	case config.EncodingGBK:
		return "GBK"
	case config.EncodingGB18030:
		return "GB18030"
	case config.EncodingBig5:
		return "Big5"
	case config.EncodingSJIS:
		return "Shift_JIS"
	case config.EncodingEUCJP:
		return "EUC-JP"
	case config.EncodingEUCKR:
		return "EUC-KR"
	case config.EncodingLatin1:
		return "ISO-8859-1"
	case config.EncodingWin1252:
		return "Windows-1252"
	}
	return string(e)
}

// lookup is the converter of e: nil for UTF-8 and for names it does not
// know, which pass bytes through.
func lookup(e config.Encoding) encoding.Encoding {
	switch e {
	case config.EncodingGBK:
		return simplifiedchinese.GBK
	case config.EncodingGB18030:
		return simplifiedchinese.GB18030
	case config.EncodingBig5:
		return traditionalchinese.Big5
	case config.EncodingSJIS:
		return japanese.ShiftJIS
	case config.EncodingEUCJP:
		return japanese.EUCJP
	case config.EncodingEUCKR:
		return korean.EUCKR
	case config.EncodingLatin1:
		return charmap.ISO8859_1
	case config.EncodingWin1252:
		return charmap.Windows1252
	}
	return nil
}

// Wrap is rw converted from and to e: what is read from it is decoded
// to UTF-8, and what is written to it, UTF-8, is encoded. Bytes e cannot
// decode read as U+FFFD; characters it cannot encode are written as
// "?". For UTF-8 Wrap is rw itself.
//
// A Resize(cols, rows int) error method of rw is kept, as the terminal
// tells the server its size through it.
func Wrap(rw io.ReadWriteCloser, e config.Encoding) io.ReadWriteCloser {
	enc := lookup(e)
	if enc == nil {
		return rw
	}
	c := &conn{
		rw:  rw,
		r:   transform.NewReader(rw, enc.NewDecoder()),
		enc: enc.NewEncoder(),
	}
	if r, ok := rw.(resizer); ok {
		return &resizeConn{c, r}
	}
	return c
}

type resizer interface{ Resize(cols, rows int) error }

// conn is a connection converted to and from UTF-8.
type conn struct {
	rw io.ReadWriteCloser
	r  io.Reader // rw, decoded

	mu   sync.Mutex // Write's
	enc  *encoding.Encoder
	tail []byte // a character cut off at the end of the last Write
}

// Read reads rw decoded. A character cut between two reads of rw is
// kept until the rest arrives.
func (c *conn) Read(p []byte) (int, error) { return c.r.Read(p) }

// Write encodes p and writes it to rw. A character cut off at the end of
// p waits for the rest, which the next Write starts with. It reports all
// of p written once rw took the encoded bytes.
func (c *conn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	buf := append(c.tail, p...)
	cut := len(buf)
	for i := len(buf) - 1; i >= 0 && i >= len(buf)-utf8.UTFMax; i-- {
		if utf8.RuneStart(buf[i]) {
			if !utf8.FullRune(buf[i:]) {
				cut = i
			}
			break
		}
	}
	c.tail = append([]byte(nil), buf[cut:]...)
	if _, err := c.rw.Write(c.encode(buf[:cut])); err != nil {
		return 0, err
	}
	return len(p), nil
}

// encode is s, UTF-8, in the connection's encoding, with "?" for each
// character the encoding lacks and each byte that is not UTF-8. It is
// not encoding.ReplaceUnsupported, whose replacement is the encoding's
// own, 0x1A in most: Ctrl+Z to a shell.
func (c *conn) encode(s []byte) []byte {
	if utf8.Valid(s) {
		if out, err := c.enc.Bytes(s); err == nil {
			return out
		}
	}
	var out []byte
	for len(s) > 0 {
		r, n := utf8.DecodeRune(s)
		b, err := c.enc.Bytes(s[:n])
		if err != nil || r == utf8.RuneError && n == 1 {
			b = []byte{'?'}
		}
		out = append(out, b...)
		s = s[n:]
	}
	return out
}

func (c *conn) Close() error { return c.rw.Close() }

// resizeConn is a conn whose connection takes the terminal's size.
type resizeConn struct {
	*conn
	r resizer
}

func (c *resizeConn) Resize(cols, rows int) error { return c.r.Resize(cols, rows) }
