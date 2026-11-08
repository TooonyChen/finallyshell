package cloudsync

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// S3 keeps the file as an object in an S3-compatible bucket: Amazon S3,
// Cloudflare R2, Backblaze B2, MinIO and the like. Requests use
// path-style URLs, Endpoint/Bucket/Key, signed with AWS Signature
// Version 4.
type S3 struct {
	// Endpoint is the service's URL, as https://s3.us-east-1.amazonaws.com
	// or https://<account>.r2.cloudflarestorage.com; without a scheme,
	// https:// is assumed.
	Endpoint string
	// Region is the bucket's region, "" for "us-east-1". Cloudflare R2
	// takes "auto".
	Region string
	// Bucket is the bucket's name.
	Bucket string
	// Key is the object's key, "" for FileName.
	Key string
	// AccessKeyID and SecretAccessKey are the credentials that sign the
	// requests.
	AccessKeyID     string
	SecretAccessKey string
	// Client is the HTTP client, nil for one with a 60-second timeout.
	Client *http.Client

	// now is the clock the signatures use, nil for time.Now; tests set it.
	now func() time.Time
}

// emptyHash is the hex SHA-256 of no bytes, the payload hash of a GET.
const emptyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// Get reads the object: ErrNotFound when there is none. Its Version is
// the object's ETag.
func (s *S3) Get(ctx context.Context) ([]byte, Version, error) {
	resp, err := s.do(ctx, http.MethodGet, nil, nil)
	if err != nil {
		return nil, Version{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, Version{}, s.error(http.MethodGet, resp, ErrNotFound)
	default:
		return nil, Version{}, s.error(http.MethodGet, resp, nil)
	}
	data, err := readBody(resp.Body)
	if err != nil {
		return nil, Version{}, err
	}
	return data, Version{Tag: resp.Header.Get("ETag")}, nil
}

// Put writes the object, with If-Match when prev has a Tag: ErrConflict
// when the object changed since. A provider that answers 501 to
// If-Match, as some do not support conditional writes, is asked once
// more without it. The new Version is the response's ETag.
func (s *S3) Put(ctx context.Context, data []byte, prev *Version) (Version, error) {
	ifMatch := ""
	if prev != nil {
		ifMatch = prev.Tag
	}
	for {
		hdr := http.Header{"Content-Type": {"application/json"}}
		if ifMatch != "" {
			hdr.Set("If-Match", ifMatch)
		}
		resp, err := s.do(ctx, http.MethodPut, data, hdr)
		if err != nil {
			return Version{}, err
		}
		switch resp.StatusCode {
		case http.StatusOK, http.StatusCreated, http.StatusNoContent:
			drain(resp.Body)
			return Version{Tag: resp.Header.Get("ETag")}, nil
		case http.StatusPreconditionFailed, http.StatusConflict:
			// 409 is S3's ConditionalRequestConflict: another
			// conditional write to the object was under way.
			err := s.error(http.MethodPut, resp, ErrConflict)
			drain(resp.Body)
			return Version{}, err
		case http.StatusNotImplemented:
			if ifMatch != "" {
				drain(resp.Body)
				ifMatch = ""
				continue
			}
		}
		err = s.error(http.MethodPut, resp, nil)
		drain(resp.Body)
		return Version{}, err
	}
}

// do makes a signed request for the object with body (none when nil) and
// the headers hdr.
func (s *S3) do(ctx context.Context, method string, body []byte, hdr http.Header) (*http.Response, error) {
	u, err := s.objectURL()
	if err != nil {
		return nil, err
	}
	if s.AccessKeyID == "" || s.SecretAccessKey == "" {
		return nil, fmt.Errorf("%w: no S3 access key", ErrAuth)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cloudsync: s3: %w", err)
	}
	if body == nil {
		req.Body, req.GetBody, req.ContentLength = nil, nil, 0
	}
	// The path is sent as signed, segment by segment.
	req.URL.Path, req.URL.RawPath = u.Path, u.RawPath
	for k, vs := range hdr {
		req.Header[k] = vs
	}
	req.Header.Set("User-Agent", userAgent)
	sum := sha256.Sum256(body)
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	signV4(req, hex.EncodeToString(sum[:]), s.AccessKeyID, s.SecretAccessKey, s.region(), "s3", now())
	resp, err := clientOr(s.Client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudsync: s3: %w", err)
	}
	return resp, nil
}

// error is the error of a response with an unexpected status, of kind
// when it is not nil, else ErrAuth for 401 and 403.
func (s *S3) error(method string, resp *http.Response, kind error) error {
	if kind == nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		kind = ErrAuth
	}
	return newHTTPError("s3", method, resp, kind, s.SecretAccessKey)
}

// region is Region, or "us-east-1".
func (s *S3) region() string {
	if s.Region == "" {
		return "us-east-1"
	}
	return s.Region
}

// objectURL is the object's path-style URL, its RawPath encoded as
// Signature Version 4 encodes it.
func (s *S3) objectURL() (*url.URL, error) {
	ep := strings.TrimRight(strings.TrimSpace(s.Endpoint), "/")
	if ep == "" {
		return nil, errors.New("cloudsync: s3: no endpoint")
	}
	if !strings.Contains(ep, "://") {
		ep = "https://" + ep
	}
	u, err := url.Parse(ep)
	if err != nil {
		return nil, fmt.Errorf("cloudsync: s3: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("cloudsync: s3: bad endpoint %q", s.Endpoint)
	}
	if s.Bucket == "" {
		return nil, errors.New("cloudsync: s3: no bucket")
	}
	key := strings.TrimLeft(s.Key, "/")
	if key == "" {
		key = FileName
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + s.Bucket + "/" + key
	u.RawPath = awsEscapePath(u.Path)
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

// signSkip are the headers signV4 leaves out of the signature: ones a
// proxy or the transport may change or add.
var signSkip = map[string]bool{
	"authorization":   true,
	"user-agent":      true,
	"accept-encoding": true,
	"content-length":  true,
	"connection":      true,
	"expect":          true,
	"x-amzn-trace-id": true,
}

// signV4 signs req with AWS Signature Version 4, setting its X-Amz-Date,
// X-Amz-Content-Sha256 and Authorization headers. payloadHash is the hex
// SHA-256 of the body. The signature covers the host and every header
// set on req but those in signSkip.
func signV4(req *http.Request, payloadHash, accessKey, secret, region, service string, t time.Time) {
	t = t.UTC()
	amzDate := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	values := map[string]string{"host": host}
	for k, vs := range req.Header {
		k = strings.ToLower(k)
		if signSkip[k] || k == "host" {
			continue
		}
		trimmed := make([]string, len(vs))
		for i, v := range vs {
			trimmed[i] = strings.Join(strings.Fields(v), " ")
		}
		values[k] = strings.Join(trimmed, ",")
	}
	names := make([]string, 0, len(values))
	for k := range values {
		names = append(names, k)
	}
	slices.Sort(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + values[k] + "\n")
	}
	signed := strings.Join(names, ";")

	canonical := strings.Join([]string{
		req.Method,
		awsEscapePath(req.URL.Path),
		awsCanonicalQuery(req.URL.Query()),
		canonHeaders.String(),
		signed,
		payloadHash,
	}, "\n")
	scope := day + "/" + region + "/" + service + "/aws4_request"
	sum := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])

	key := hmacSHA256([]byte("AWS4"+secret), day)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, service)
	key = hmacSHA256(key, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(key, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+
		", SignedHeaders="+signed+", Signature="+sig)
}

// hmacSHA256 is the HMAC-SHA256 of msg under key.
func hmacSHA256(key []byte, msg string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return h.Sum(nil)
}

// awsEscapePath encodes each segment of a path as Signature Version 4
// does, keeping the slashes between them: "/" for an empty path.
func awsEscapePath(p string) string {
	if p == "" {
		return "/"
	}
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		segs[i] = awsEscape(seg)
	}
	return strings.Join(segs, "/")
}

// awsCanonicalQuery is a query in Signature Version 4's canonical form:
// keys and values encoded, sorted by key, then by value.
func awsCanonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var parts []string
	for _, k := range keys {
		vs := slices.Clone(q[k])
		slices.Sort(vs)
		for _, v := range vs {
			parts = append(parts, awsEscape(k)+"="+awsEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

// awsEscape percent-encodes every byte of s but the unreserved
// characters A-Z, a-z, 0-9, '-', '_', '.' and '~', with uppercase hex.
func awsEscape(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&15])
	}
	return b.String()
}
