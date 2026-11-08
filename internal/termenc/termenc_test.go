package termenc

import (
	"bytes"
	"io"
	"testing"
	"testing/iotest"

	"finallyshell/internal/config"
)

// fakeConn is a connection whose reads come from r and whose writes are
// kept.
type fakeConn struct {
	r       io.Reader
	written bytes.Buffer
	size    [2]int
	closed  bool
}

func (c *fakeConn) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c *fakeConn) Write(p []byte) (int, error) { return c.written.Write(p) }
func (c *fakeConn) Close() error                { c.closed = true; return nil }

type resizeFake struct{ fakeConn }

func (c *resizeFake) Resize(cols, rows int) error { c.size = [2]int{cols, rows}; return nil }

// "你好，世界" in GBK.
var gbkHello = []byte{0xc4, 0xe3, 0xba, 0xc3, 0xa3, 0xac, 0xca, 0xc0, 0xbd, 0xe7}

func TestUTF8PassesThrough(t *testing.T) {
	c := &fakeConn{r: bytes.NewReader(nil)}
	for _, e := range []config.Encoding{config.EncodingUTF8, "", "bogus"} {
		if Wrap(c, e) != io.ReadWriteCloser(c) {
			t.Errorf("%q: wrapped", e)
		}
	}
}

// Output decodes even when every character is cut between two reads;
// bytes GBK has no character for read as U+FFFD.
func TestDecode(t *testing.T) {
	src := append([]byte("$ ls\r\n\x1b[1;34m"), gbkHello...)
	src = append(src, "\x1b[0m\r\n"...)
	c := &fakeConn{r: iotest.OneByteReader(bytes.NewReader(src))}
	got, err := io.ReadAll(Wrap(c, config.EncodingGBK))
	if err != nil {
		t.Fatal(err)
	}
	if want := "$ ls\r\n\x1b[1;34m你好，世界\x1b[0m\r\n"; string(got) != want {
		t.Errorf("read %q, want %q", got, want)
	}

	c = &fakeConn{r: bytes.NewReader([]byte{'a', 0xff, 'b'})}
	got, _ = io.ReadAll(Wrap(c, config.EncodingGBK))
	if string(got) != "a�b" {
		t.Errorf("invalid byte read %q", got)
	}
}

// Input encodes even when a character is cut between two writes;
// characters GBK lacks are sent as "?".
func TestEncode(t *testing.T) {
	c := &fakeConn{r: bytes.NewReader(nil)}
	w := Wrap(c, config.EncodingGBK)
	in := []byte("你好，世界")
	for i, b := range in {
		if n, err := w.Write([]byte{b}); n != 1 || err != nil {
			t.Fatalf("write %d: %d, %v", i, n, err)
		}
	}
	if !bytes.Equal(c.written.Bytes(), gbkHello) {
		t.Errorf("wrote % x, want % x", c.written.Bytes(), gbkHello)
	}

	c.written.Reset()
	w.Write([]byte("a😀b\x1b[A\x7f\xff中"))
	if got := c.written.String(); got != "a?b\x1b[A\x7f?\xd6\xd0" {
		t.Errorf("wrote %q", got)
	}

	// GB18030 has every character, but not bytes that are not UTF-8.
	c18 := &fakeConn{r: bytes.NewReader(nil)}
	Wrap(c18, config.EncodingGB18030).Write([]byte("x\xffy"))
	if got := c18.written.String(); got != "x?y" {
		t.Errorf("GB18030 wrote %q", got)
	}

	w.Close()
	if !c.closed {
		t.Error("not closed")
	}
}

func TestEncodings(t *testing.T) {
	for _, tc := range []struct {
		e    config.Encoding
		text string
		raw  []byte
	}{
		{config.EncodingGB18030, "€", []byte{0xa2, 0xe3}},
		{config.EncodingBig5, "中文", []byte{0xa4, 0xa4, 0xa4, 0xe5}},
		{config.EncodingSJIS, "日本", []byte{0x93, 0xfa, 0x96, 0x7b}},
		{config.EncodingEUCJP, "日本", []byte{0xc6, 0xfc, 0xcb, 0xdc}},
		{config.EncodingEUCKR, "한국", []byte{0xc7, 0xd1, 0xb1, 0xb9}},
		{config.EncodingLatin1, "café", []byte{'c', 'a', 'f', 0xe9}},
		{config.EncodingWin1252, "€", []byte{0x80}},
	} {
		t.Run(Label(tc.e), func(t *testing.T) {
			c := &fakeConn{r: bytes.NewReader(tc.raw)}
			w := Wrap(c, tc.e)
			got, _ := io.ReadAll(w)
			if string(got) != tc.text {
				t.Errorf("read %q, want %q", got, tc.text)
			}
			w.Write([]byte(tc.text))
			if !bytes.Equal(c.written.Bytes(), tc.raw) {
				t.Errorf("wrote % x, want % x", c.written.Bytes(), tc.raw)
			}
		})
	}
	for _, e := range config.Encodings {
		if e != config.EncodingUTF8 && lookup(e) == nil {
			t.Errorf("no converter for %q", e)
		}
	}
}

// The terminal's size still reaches a connection that takes it.
func TestResizeKept(t *testing.T) {
	c := &resizeFake{fakeConn{r: bytes.NewReader(nil)}}
	r, ok := Wrap(c, config.EncodingGBK).(resizer)
	if !ok {
		t.Fatal("no Resize")
	}
	r.Resize(120, 40)
	if c.size != [2]int{120, 40} {
		t.Errorf("size %v", c.size)
	}
	if _, ok := Wrap(&fakeConn{r: bytes.NewReader(nil)}, config.EncodingGBK).(resizer); ok {
		t.Error("Resize on a connection without one")
	}
}
