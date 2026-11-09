package app

import (
	"errors"
	"net"
	"net/http"
	"testing"

	"finallyshell/internal/geo"
)

// The error after every source failed names each host and what it
// answered, in the app's language.
func TestGeoErrorText(t *testing.T) {
	a, _ := testApp(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	_, refused := http.Get("http://" + addr + "/v4.xdb")
	if refused == nil {
		t.Fatal("not refused")
	}
	err = &geo.DownloadError{Sources: []*geo.SourceError{
		{Host: "raw.githubusercontent.com", Err: refused},
		{Host: "cdn.jsdelivr.net", Err: &geo.StatusError{Code: 403, Status: "403 Forbidden"}},
		{Host: "a.example", Err: geo.ErrNoAnswer},
		{Host: "b.example", Err: geo.ErrChecksum},
		{Host: "c.example", Err: errors.New("tls: bad certificate")},
	}}
	want := "raw.githubusercontent.com: connection refused; cdn.jsdelivr.net: 403 Forbidden; a.example: no answer in time; " +
		"b.example: the file is not the one expected (its checksum does not match); c.example: tls: bad certificate"
	if got := geoErrorText(err); got != want {
		t.Errorf("%q\nwant %q", got, want)
	}
	a.setLanguage("zh-Hans")
	want = "raw.githubusercontent.com：连接被拒绝；cdn.jsdelivr.net：403 Forbidden；a.example：没有及时响应；" +
		"b.example：文件与预期不符（校验和不匹配）；c.example：tls: bad certificate"
	if got := geoErrorText(err); got != want {
		t.Errorf("%q\nwant %q", got, want)
	}
	a.setLanguage("en")
	// An error that is not of the sources, such as the disk's.
	if got := geoErrorText(errors.New("disk full")); got != "disk full" {
		t.Errorf("%q", got)
	}
}
