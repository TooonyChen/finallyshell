package cloudsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// WebDAV keeps the file in a WebDAV folder, such as Nutstore's
// (Jianguoyun), Nextcloud's or a NAS's, logging in with HTTP basic auth.
type WebDAV struct {
	// URL is the folder's URL, as https://dav.jianguoyun.com/dav/finallyshell/;
	// the file is FileName in it. Put creates the folder when it is
	// missing.
	URL string
	// User and Password log in; both empty sends no credentials.
	User     string
	Password string
	// Client is the HTTP client, nil for one with a 60-second timeout.
	Client *http.Client
}

// errFolderMissing is a PUT the server refused for want of the folder.
var errFolderMissing = errors.New("cloudsync: webdav: the folder is missing")

// Get reads the file: ErrNotFound when there is none. Its Version is the
// response's ETag, "" when the server sent none.
func (w *WebDAV) Get(ctx context.Context) ([]byte, Version, error) {
	u, err := w.fileURL()
	if err != nil {
		return nil, Version{}, err
	}
	resp, err := w.do(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, Version{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusGone:
		return nil, Version{}, newHTTPError("webdav", http.MethodGet, resp, ErrNotFound, w.Password)
	default:
		return nil, Version{}, w.error(http.MethodGet, resp)
	}
	data, err := readBody(resp.Body)
	if err != nil {
		return nil, Version{}, err
	}
	return data, Version{Tag: resp.Header.Get("ETag")}, nil
}

// Put writes the file, with If-Match when prev has a Tag: ErrConflict
// when the file changed since. Without one the write is not conditional;
// If-None-Match is not sent, as some servers mishandle it. When the
// server answers that the folder is missing (409, or 404), Put creates
// it and tries once more. The new Version is the response's ETag, ""
// when the server sent none.
func (w *WebDAV) Put(ctx context.Context, data []byte, prev *Version) (Version, error) {
	u, err := w.fileURL()
	if err != nil {
		return Version{}, err
	}
	v, err := w.put(ctx, u, data, prev)
	if errors.Is(err, errFolderMissing) {
		if err := w.mkcol(ctx); err != nil {
			return Version{}, err
		}
		v, err = w.put(ctx, u, data, prev)
	}
	return v, err
}

// put makes one PUT of the file: errFolderMissing when the server wants
// the folder first.
func (w *WebDAV) put(ctx context.Context, u string, data []byte, prev *Version) (Version, error) {
	resp, err := w.do(ctx, http.MethodPut, u, data, func(req *http.Request) {
		req.Header.Set("Content-Type", "application/json")
		// A weak ETag never matches If-Match, which compares strongly;
		// such a write cannot be conditional.
		if prev != nil && prev.Tag != "" && !strings.HasPrefix(prev.Tag, "W/") {
			req.Header.Set("If-Match", prev.Tag)
		}
	})
	if err != nil {
		return Version{}, err
	}
	defer drain(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return Version{Tag: resp.Header.Get("ETag")}, nil
	case http.StatusPreconditionFailed:
		return Version{}, newHTTPError("webdav", http.MethodPut, resp, ErrConflict, w.Password)
	case http.StatusConflict, http.StatusNotFound:
		return Version{}, errFolderMissing
	default:
		return Version{}, w.error(http.MethodPut, resp)
	}
}

// mkcol creates the folder, and each folder above it, from the top. A 405
// is a folder that is there. Other failures above the folder itself are
// passed over, as the top of a server's tree may refuse MKCOL (403) where
// the folders exist; the folder's own is returned.
func (w *WebDAV) mkcol(ctx context.Context) error {
	folder, err := w.folderURL()
	if err != nil {
		return err
	}
	segs := strings.Split(strings.Trim(folder.EscapedPath(), "/"), "/")
	prefix := ""
	for i, seg := range segs {
		if seg == "" {
			continue
		}
		prefix += "/" + seg
		escaped := prefix + "/"
		p, err := url.PathUnescape(escaped)
		if err != nil {
			return fmt.Errorf("cloudsync: webdav: %w", err)
		}
		u := *folder
		u.Path, u.RawPath, u.RawQuery = p, escaped, ""
		last := i == len(segs)-1
		resp, err := w.do(ctx, "MKCOL", u.String(), nil)
		if err != nil {
			return err
		}
		switch {
		case resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK ||
			resp.StatusCode == http.StatusMethodNotAllowed:
		case resp.StatusCode == http.StatusUnauthorized || last:
			err := w.error("MKCOL", resp)
			drain(resp.Body)
			return err
		}
		drain(resp.Body)
	}
	return nil
}

// do makes a request with the credentials, the body when not nil, and
// what each of edit sets.
func (w *WebDAV) do(ctx context.Context, method, u string, body []byte, edit ...func(*http.Request)) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return nil, fmt.Errorf("cloudsync: webdav: %w", err)
	}
	if w.User != "" || w.Password != "" {
		req.SetBasicAuth(w.User, w.Password)
	}
	req.Header.Set("User-Agent", userAgent)
	for _, f := range edit {
		f(req)
	}
	resp, err := clientOr(w.Client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudsync: webdav: %w", redactURLError(err))
	}
	return resp, nil
}

// error is the error of a response with an unexpected status.
func (w *WebDAV) error(method string, resp *http.Response) error {
	var kind error
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		kind = ErrAuth
	}
	return newHTTPError("webdav", method, resp, kind, w.Password)
}

// folderURL is URL parsed, ending in a slash.
func (w *WebDAV) folderURL() (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(w.URL))
	if err != nil {
		return nil, fmt.Errorf("cloudsync: webdav: %w", redactURLError(err))
	}
	if u.Scheme != "https" && u.Scheme != "http" || u.Host == "" {
		return nil, errors.New("cloudsync: webdav: the URL must start with https:// or http://")
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
		if u.RawPath != "" {
			u.RawPath += "/"
		}
	}
	return u, nil
}

// fileURL is the file's URL: FileName in the folder.
func (w *WebDAV) fileURL() (string, error) {
	u, err := w.folderURL()
	if err != nil {
		return "", err
	}
	return u.JoinPath(FileName).String(), nil
}

// redactURLError hides the password of a URL in err, for a URL with
// user:password@ in it.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		if u, perr := url.Parse(ue.URL); perr == nil {
			ue.URL = u.Redacted()
		} else {
			ue.URL = "(the URL)"
		}
	}
	return err
}
