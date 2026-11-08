package cloudsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// Gist keeps the file in a secret GitHub gist, as the file FileName.
//
// A Gist is not safe for concurrent use: Get and Put set ID.
type Gist struct {
	// Token is a GitHub token allowed to read and write gists: a classic
	// token with the gist scope, or a fine-grained one with Gists read
	// and write.
	Token string
	// ID is the gist's ID. Empty, Get looks for the newest of the user's
	// gists that has the file, and Put creates one; both set it.
	ID string
	// Client is the HTTP client, nil for one with a 60-second timeout.
	Client *http.Client
	// BaseURL is the GitHub API's URL, "" for "https://api.github.com".
	BaseURL string
}

// gistDescription is the description of the gists Put creates.
const gistDescription = "Finallyshell sync (encrypted, do not edit)"

// gistMaxPages is how many pages of the user's gists Get looks through
// for the file, 100 gists a page.
const gistMaxPages = 10

// gistFile is a file of a gist, as the API gives it.
type gistFile struct {
	Content string `json:"content"`
	// Truncated is set when Content is not all of the file, which then
	// is at RawURL.
	Truncated bool   `json:"truncated"`
	RawURL    string `json:"raw_url"`
}

// gistDoc is a gist, as the API gives it.
type gistDoc struct {
	ID        string               `json:"id"`
	UpdatedAt string               `json:"updated_at"`
	Files     map[string]*gistFile `json:"files"`
	History   []struct {
		Version string `json:"version"`
	} `json:"history"`
}

// version is the gist's Version: its newest revision, else when it was
// last changed.
func (d *gistDoc) version() Version {
	if len(d.History) > 0 && d.History[0].Version != "" {
		return Version{Tag: d.History[0].Version}
	}
	return Version{Tag: d.UpdatedAt}
}

// Get reads the file from the gist, finding the gist first when ID is
// empty. It returns ErrNotFound when there is no such gist, or the gist
// has no such file; ID is kept either way.
func (g *Gist) Get(ctx context.Context) ([]byte, Version, error) {
	if g.Token == "" {
		return nil, Version{}, fmt.Errorf("%w: no GitHub token", ErrAuth)
	}
	if g.ID == "" {
		id, err := g.find(ctx)
		if err != nil {
			return nil, Version{}, err
		}
		g.ID = id
	}
	doc, err := g.fetch(ctx)
	if err != nil {
		return nil, Version{}, err
	}
	f := doc.Files[FileName]
	if f == nil {
		return nil, Version{}, ErrNotFound
	}
	data := []byte(f.Content)
	if f.Truncated {
		if data, err = g.raw(ctx, f.RawURL); err != nil {
			return nil, Version{}, err
		}
	}
	return data, doc.version(), nil
}

// Put writes the file to the gist, creating the gist when ID is empty or
// the gist is gone, and setting ID to it. GitHub has no conditional
// write, so when prev has a Tag, Put reads the gist's version first and
// returns ErrConflict when it is not prev's: that narrows the race to the
// time between the two requests.
func (g *Gist) Put(ctx context.Context, data []byte, prev *Version) (Version, error) {
	if g.Token == "" {
		return Version{}, fmt.Errorf("%w: no GitHub token", ErrAuth)
	}
	if !utf8.Valid(data) {
		// A gist holds text: JSON would turn invalid UTF-8 into U+FFFD.
		return Version{}, errors.New("cloudsync: gist: the file is not UTF-8 text")
	}
	if g.ID == "" {
		return g.create(ctx, data)
	}
	if prev == nil || prev.Tag != "" {
		cur, err := g.fetch(ctx)
		switch {
		case errors.Is(err, ErrNotFound) && prev == nil:
			return g.create(ctx, data)
		case errors.Is(err, ErrNotFound):
			// The gist was deleted since Get read it.
			return Version{}, ErrConflict
		case err != nil:
			return Version{}, err
		case prev == nil && cur.Files[FileName] != nil:
			// The file was made since Get found none.
			return Version{}, ErrConflict
		case prev != nil && cur.version().Tag != prev.Tag:
			return Version{}, ErrConflict
		}
	}
	doc, err := g.send(ctx, http.MethodPatch, g.base()+"/gists/"+url.PathEscape(g.ID), map[string]any{
		"files": map[string]any{FileName: map[string]string{"content": string(data)}},
	}, http.StatusOK)
	if errors.Is(err, ErrNotFound) {
		return Version{}, ErrConflict
	}
	if err != nil {
		return Version{}, err
	}
	return doc.version(), nil
}

// create makes a secret gist with the file and sets ID to it.
func (g *Gist) create(ctx context.Context, data []byte) (Version, error) {
	doc, err := g.send(ctx, http.MethodPost, g.base()+"/gists", map[string]any{
		"description": gistDescription,
		"public":      false,
		"files":       map[string]any{FileName: map[string]string{"content": string(data)}},
	}, http.StatusCreated)
	if err != nil {
		return Version{}, err
	}
	if doc.ID == "" {
		return Version{}, errors.New("cloudsync: gist: GitHub gave no ID for the new gist")
	}
	g.ID = doc.ID
	return doc.version(), nil
}

// find is the ID of the newest of the user's gists that has the file,
// looking through at most gistMaxPages pages: ErrNotFound for none.
func (g *Gist) find(ctx context.Context) (string, error) {
	base, err := url.Parse(g.base())
	if err != nil {
		return "", fmt.Errorf("cloudsync: gist: %w", err)
	}
	next := g.base() + "/gists?per_page=100"
	var best string
	var bestAt time.Time
	for page := 0; next != "" && page < gistMaxPages; page++ {
		resp, err := g.do(ctx, http.MethodGet, next, nil, true)
		if err != nil {
			return "", err
		}
		if resp.StatusCode != http.StatusOK {
			err := g.error(http.MethodGet, resp)
			resp.Body.Close()
			return "", err
		}
		b, err := readBody(resp.Body)
		link := resp.Header.Get("Link")
		resp.Body.Close()
		if err != nil {
			return "", err
		}
		var gists []gistDoc
		if err := json.Unmarshal(b, &gists); err != nil {
			return "", fmt.Errorf("cloudsync: gist: reading the list of gists: %w", err)
		}
		for _, d := range gists {
			if _, ok := d.Files[FileName]; !ok || d.ID == "" {
				continue
			}
			at, _ := time.Parse(time.RFC3339, d.UpdatedAt)
			if best == "" || at.After(bestAt) {
				best, bestAt = d.ID, at
			}
		}
		next = nextLink(link)
		// The token goes only to the API's own host.
		if u, err := url.Parse(next); err != nil || u.Host != base.Host || u.Scheme != base.Scheme {
			next = ""
		}
	}
	if best == "" {
		return "", ErrNotFound
	}
	return best, nil
}

// nextLink is the rel="next" URL of a Link header, "" for none.
func nextLink(h string) string {
	for part := range strings.SplitSeq(h, ",") {
		segs := strings.Split(part, ";")
		target := strings.TrimSpace(segs[0])
		if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		for _, p := range segs[1:] {
			if strings.ReplaceAll(strings.TrimSpace(p), " ", "") == `rel="next"` {
				return target[1 : len(target)-1]
			}
		}
	}
	return ""
}

// fetch reads the gist ID: ErrNotFound when there is none.
func (g *Gist) fetch(ctx context.Context) (*gistDoc, error) {
	return g.send(ctx, http.MethodGet, g.base()+"/gists/"+url.PathEscape(g.ID), nil, http.StatusOK)
}

// raw reads a truncated file from its raw URL. The token goes with it
// only to GitHub's hosts.
func (g *Gist) raw(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("cloudsync: gist: bad raw URL %q", rawURL)
	}
	base, _ := url.Parse(g.base())
	host := u.Hostname()
	auth := (base != nil && u.Host == base.Host) || host == "githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com")
	resp, err := g.do(ctx, http.MethodGet, rawURL, nil, auth)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, g.error(http.MethodGet, resp)
	}
	return readBody(resp.Body)
}

// send makes a request with body as JSON (none when nil), and reads the
// gist the response holds, expecting the status want.
func (g *Gist) send(ctx context.Context, method, u string, body any, want int) (*gistDoc, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	resp, err := g.do(ctx, method, u, r, true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return nil, g.error(method, resp)
	}
	b, err := readBody(resp.Body)
	if err != nil {
		return nil, err
	}
	var doc gistDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("cloudsync: gist: reading the gist: %w", err)
	}
	return &doc, nil
}

// do makes a request with GitHub's headers, with the token when auth is
// set.
func (g *Gist) do(ctx context.Context, method, u string, body io.Reader, auth bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("cloudsync: gist: %w", err)
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := clientOr(g.Client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudsync: gist: %w", err)
	}
	return resp, nil
}

// error is the error of a response with an unexpected status. A 403 or
// 429 with no requests left is GitHub's rate limit, not the token.
func (g *Gist) error(method string, resp *http.Response) error {
	var kind error
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		kind = ErrAuth
	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") != "0" {
			kind = ErrAuth
		}
	case http.StatusNotFound:
		kind = ErrNotFound
	}
	return newHTTPError("gist", method, resp, kind, g.Token)
}

// base is BaseURL without a trailing slash, or GitHub's API.
func (g *Gist) base() string {
	if g.BaseURL == "" {
		return "https://api.github.com"
	}
	return strings.TrimRight(g.BaseURL, "/")
}
