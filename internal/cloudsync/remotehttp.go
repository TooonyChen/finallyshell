package cloudsync

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// maxResponse is the most of a response body the remotes read: the file is
// a small JSON document, so anything larger is not it.
const maxResponse = 16 << 20

// userAgent is the User-Agent of every request; GitHub refuses requests
// without one.
const userAgent = "finallyshell"

// defaultClient is the HTTP client of a remote whose Client is nil.
var defaultClient = &http.Client{Timeout: 60 * time.Second}

// clientOr is c, else defaultClient.
func clientOr(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return defaultClient
}

// readBody reads a response body, failing when it is over maxResponse.
func readBody(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxResponse {
		return nil, fmt.Errorf("cloudsync: the response is over %d bytes", maxResponse)
	}
	return b, nil
}

// drain reads what is left of a body, up to a limit, and closes it, so
// that the connection can be used again.
func drain(body io.ReadCloser) {
	io.Copy(io.Discard, io.LimitReader(body, 64<<10))
	body.Close()
}

// httpError is a response with a status the remote did not expect: the
// status and the provider's message, which S3 and GitHub fill with why.
// It unwraps to kind, one of ErrAuth, ErrNotFound and ErrConflict, or nil.
type httpError struct {
	remote string
	method string
	status string
	msg    string
	kind   error
}

func (e *httpError) Error() string {
	detail := e.remote + ": " + e.method + ": " + e.status
	if e.msg != "" {
		detail += ": " + e.msg
	}
	if e.kind != nil {
		return e.kind.Error() + " (" + detail + ")"
	}
	return "cloudsync: " + detail
}

func (e *httpError) Unwrap() error { return e.kind }

// maxErrorText is how much of a response body an httpError keeps.
const maxErrorText = 200

// newHTTPError makes the httpError of resp, reading the start of its
// body. secrets are the remote's credentials: they are cut out of the
// message, should a provider echo one back.
func newHTTPError(remote, method string, resp *http.Response, kind error, secrets ...string) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	msg := errorText(b)
	for _, s := range secrets {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "***")
		}
	}
	return &httpError{remote: remote, method: method, status: resp.Status, msg: msg, kind: kind}
}

// errorText is the message of an error response body: GitHub's JSON
// "message", S3's XML Code and Message, or else the body itself, on one
// line, at most maxErrorText bytes.
func errorText(b []byte) string {
	var gh struct {
		Message string `json:"message"`
	}
	var s3 struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	var s string
	switch {
	case json.Unmarshal(b, &gh) == nil && gh.Message != "":
		s = gh.Message
	case xml.Unmarshal(b, &s3) == nil && s3.Code != "":
		s = s3.Code
		if s3.Message != "" {
			s += ": " + s3.Message
		}
	default:
		s = string(b)
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxErrorText {
		s = s[:maxErrorText]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		s += "…"
	}
	return s
}
