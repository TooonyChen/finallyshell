package localterm

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestControlRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	want := message{Kind: "start", Args: []string{`C:\Program Files\PowerShell\7\pwsh.exe`, "-Command", `Write-Output '中文 $()'; \"quoted\"`}, Dir: `D:\用户\文档`, ConsolePID: 42}
	done := make(chan error, 1)
	go func() { done <- (&channel{ReadWriteCloser: a}).send(want) }()
	got, err := (&channel{ReadWriteCloser: b}).receive()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, %v", got, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type memoryControl struct{ *bytes.Buffer }

func (memoryControl) Close() error { return nil }

func TestControlRejectsInvalidLengthsAndTruncation(t *testing.T) {
	for _, n := range []uint32{0, maxMessage + 1, 0xffffffff} {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], n)
		if _, err := (&channel{ReadWriteCloser: memoryControl{bytes.NewBuffer(b[:])}}).receive(); err == nil {
			t.Fatalf("accepted length %d", n)
		}
	}
	for _, b := range [][]byte{{1, 0}, {5, 0, 0, 0, '{'}} {
		if _, err := (&channel{ReadWriteCloser: memoryControl{bytes.NewBuffer(b)}}).receive(); err == nil {
			t.Fatal("accepted truncated frame")
		}
	}
	c := &channel{ReadWriteCloser: memoryControl{&bytes.Buffer{}}}
	if err := c.send(message{Kind: "error", Error: strings.Repeat("x", maxMessage)}); err == nil {
		t.Fatal("accepted oversized message")
	}
}

func TestControlConcurrentWriters(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := &channel{ReadWriteCloser: a}
	var wg sync.WaitGroup
	errors := make(chan error, 30)
	for i := range 30 {
		wg.Add(1)
		go func() { defer wg.Done(); errors <- c.send(message{Kind: "exit", Code: uint32(i)}) }()
	}
	seen := map[uint32]bool{}
	for range 30 {
		m, err := (&channel{ReadWriteCloser: b}).receive()
		if err != nil {
			t.Fatal(err)
		}
		if m.Kind != "exit" || seen[m.Code] {
			t.Fatalf("invalid frame %#v", m)
		}
		seen[m.Code] = true
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(b []byte) (int, error) {
	if len(b) > 2 {
		b = b[:2]
	}
	return w.Buffer.Write(b)
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

func TestControlShortWrites(t *testing.T) {
	w := &shortWriter{}
	if err := writeAll(w, []byte("abcdef")); err != nil || w.String() != "abcdef" {
		t.Fatalf("%q %v", w.String(), err)
	}
	if err := writeAll(zeroWriter{}, []byte("a")); err != io.ErrShortWrite {
		t.Fatalf("%v", err)
	}
}

func TestProcessCloseIsIdempotent(t *testing.T) {
	var closed atomic.Int32
	p := &process{closeFn: func() { closed.Add(1) }}
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() { defer wg.Done(); p.Close() }()
	}
	wg.Wait()
	if closed.Load() != 1 {
		t.Fatalf("%d closes", closed.Load())
	}
}
