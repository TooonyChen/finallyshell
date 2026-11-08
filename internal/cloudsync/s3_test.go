package cloudsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSignV4Example checks the signer against the "GET Object" example of
// AWS's Signature Version 4 documentation for S3.
func TestSignV4Example(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-9")
	req.Header.Set("User-Agent", "not signed")
	signV4(req, emptyHash, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"us-east-1", "s3", time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization:\n got %s\nwant %s", got, want)
	}
	if got := req.Header.Get("X-Amz-Date"); got != "20130524T000000Z" {
		t.Fatalf("X-Amz-Date %q", got)
	}
}

func TestAWSEscapePath(t *testing.T) {
	for in, want := range map[string]string{
		"":                               "/",
		"/bucket/finallyshell-sync.json": "/bucket/finallyshell-sync.json",
		"/b/dir/a b+c(1)~_.json":         "/b/dir/a%20b%2Bc%281%29~_.json",
		"/b/中":                           "/b/%E4%B8%AD",
	} {
		if got := awsEscapePath(in); got != want {
			t.Errorf("awsEscapePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeS3 is an S3 bucket in memory that checks signatures.
type fakeS3 struct {
	t                 *testing.T
	bucket, key, sec  string
	region            string
	noConditional     bool // answer 501 to If-Match, as some providers do
	mu                sync.Mutex
	objects           map[string][]byte
	etags             map[string]string
	n                 int
	puts, ifMatchPuts int
	srv               *httptest.Server
}

func newFakeS3(t *testing.T) *fakeS3 {
	f := &fakeS3{t: t, bucket: "my-bucket", key: "AKIDTEST", sec: "s3-secret/key", region: "auto",
		objects: map[string][]byte{}, etags: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeS3) remote(key string) *S3 {
	return &S3{
		Endpoint: f.srv.URL + "/", Region: f.region, Bucket: f.bucket, Key: key,
		AccessKeyID: f.key, SecretAccessKey: f.sec, Client: f.srv.Client(),
		now: func() time.Time { return time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC) },
	}
}

// write changes an object, as another client would.
func (f *fakeS3) write(p string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.objects[p] = data
	f.etags[p] = `"etag` + strconv.Itoa(f.n) + `"`
}

// verify checks a request's signature as S3 does: the headers it names,
// signed again with the secret.
func (f *fakeS3) verify(r *http.Request, body []byte) bool {
	auth := r.Header.Get("Authorization")
	var signed string
	for part := range strings.SplitSeq(strings.TrimPrefix(auth, "AWS4-HMAC-SHA256 "), ",") {
		if s, ok := strings.CutPrefix(strings.TrimSpace(part), "SignedHeaders="); ok {
			signed = s
		}
	}
	sum := sha256.Sum256(body)
	if r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(sum[:]) {
		f.t.Errorf("payload hash does not match the body")
		return false
	}
	at, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil {
		return false
	}
	req, _ := http.NewRequest(r.Method, "http://"+r.Host+r.URL.EscapedPath(), nil)
	for name := range strings.SplitSeq(signed, ";") {
		if name != "host" {
			req.Header[http.CanonicalHeaderKey(name)] = r.Header.Values(name)
		}
	}
	signV4(req, r.Header.Get("X-Amz-Content-Sha256"), f.key, f.sec, f.region, "s3", at)
	return req.Header.Get("Authorization") == auth
}

func (f *fakeS3) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if !f.verify(r, body) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>SignatureDoesNotMatch</Code>`+
			`<Message>The request signature we calculated does not match the signature you provided.</Message>`+
			`<AWSAccessKeyId>`+f.key+`</AWSAccessKeyId></Error>`)
		return
	}
	p := r.URL.Path
	if !strings.HasPrefix(p, "/"+f.bucket+"/") {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `<Error><Code>NoSuchBucket</Code></Error>`)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		data, ok := f.objects[p]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `<Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`)
			return
		}
		w.Header().Set("ETag", f.etags[p])
		w.Write(data)
	case http.MethodPut:
		f.puts++
		if m := r.Header.Get("If-Match"); m != "" {
			f.ifMatchPuts++
			if f.noConditional {
				w.WriteHeader(http.StatusNotImplemented)
				fmt.Fprint(w, `<Error><Code>NotImplemented</Code><Message>If-Match is not supported</Message></Error>`)
				return
			}
			if m != f.etags[p] {
				w.WriteHeader(http.StatusPreconditionFailed)
				fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code></Error>`)
				return
			}
		}
		f.n++
		f.objects[p] = body
		f.etags[p] = `"etag` + strconv.Itoa(f.n) + `"`
		w.Header().Set("ETag", f.etags[p])
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestS3RoundTrip(t *testing.T) {
	f := newFakeS3(t)
	for _, key := range []string{"", "dir/a b+c(1).json"} {
		s := f.remote(key)
		ctx := context.Background()
		if _, _, err := s.Get(ctx); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get %q with no object: %v, want ErrNotFound", key, err)
		}
		v, err := s.Put(ctx, []byte(`{"a":1}`), nil)
		if err != nil {
			t.Fatal(err)
		}
		data, v2, err := s.Get(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != `{"a":1}` || v2 != v || v.Tag == "" {
			t.Fatalf("Get %q: %s, %v; Put gave %v", key, data, v2, v)
		}
		if _, err := s.Put(ctx, []byte(`{"a":2}`), &v2); err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.objects["/my-bucket/"+FileName]; !ok {
		t.Errorf("no object at the default key; have %v", f.objects)
	}
	if _, ok := f.objects["/my-bucket/dir/a b+c(1).json"]; !ok {
		t.Errorf("no object at the key with spaces; have %v", f.objects)
	}
}

func TestS3Conflict(t *testing.T) {
	f := newFakeS3(t)
	f.write("/my-bucket/"+FileName, []byte("{}"))
	s := f.remote("")
	ctx := context.Background()
	_, v, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.write("/my-bucket/"+FileName, []byte(`{"other":1}`))
	if _, err := s.Put(ctx, []byte(`{"mine":1}`), &v); !errors.Is(err, ErrConflict) {
		t.Fatalf("Put over a changed object: %v, want ErrConflict", err)
	}
}

func TestS3NoConditionalRetries(t *testing.T) {
	f := newFakeS3(t)
	f.noConditional = true
	f.write("/my-bucket/"+FileName, []byte("{}"))
	s := f.remote("")
	ctx := context.Background()
	_, v, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.Put(ctx, []byte(`{"mine":1}`), &v)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.puts != 2 || f.ifMatchPuts != 1 || v2.Tag != f.etags["/my-bucket/"+FileName] {
		t.Fatalf("%d PUTs, %d with If-Match, tag %q; want 2, 1 and the new ETag", f.puts, f.ifMatchPuts, v2.Tag)
	}
}

func TestS3Auth(t *testing.T) {
	f := newFakeS3(t)
	s := f.remote("")
	s.SecretAccessKey = "wrong-secret"
	_, _, err := s.Get(context.Background())
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("Get with a bad secret: %v, want ErrAuth", err)
	}
	if !strings.Contains(err.Error(), "SignatureDoesNotMatch") || strings.Contains(err.Error(), s.SecretAccessKey) {
		t.Fatalf("error text %q: want S3's code and no secret", err)
	}
}

func TestS3ObjectURL(t *testing.T) {
	for _, c := range []struct{ endpoint, key, want string }{
		{"s3.example.com", "", "https://s3.example.com/b/" + FileName},
		{"https://acct.r2.cloudflarestorage.com/", "x/y.json", "https://acct.r2.cloudflarestorage.com/b/x/y.json"},
		{"http://localhost:9000", "a b.json", "http://localhost:9000/b/a%20b.json"},
	} {
		u, err := (&S3{Endpoint: c.endpoint, Bucket: "b", Key: c.key}).objectURL()
		if err != nil || u.String() != c.want {
			t.Errorf("objectURL(%q, %q) = %v, %v; want %s", c.endpoint, c.key, u, err, c.want)
		}
	}
}
