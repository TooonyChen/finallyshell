package geo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DataFile is an ip2region data file the app downloads: where from, its
// size and its SHA-256, which the app keeps so that only that file is
// ever used, whichever source serves it.
type DataFile struct {
	Name   string   // the file's name in the data folder
	URLs   []string // the sources, tried in order
	Size   int64
	SHA256 string // hex
}

// Release is the ip2region release the data files are from
// (https://github.com/lionsoul2014/ip2region/tree/v3.18.0/data). A newer
// release is a change of it and of the sizes and checksums below.
const Release = "v3.18.0"

// githubURL is name in Release on GitHub.
func githubURL(name string) string {
	return "https://raw.githubusercontent.com/lionsoul2014/ip2region/" + Release + "/data/" + name
}

// jsDelivrURL is name in Release on jsDelivr, which mirrors GitHub and is
// often reachable where GitHub is not, as in mainland China. It serves
// GitHub's files of up to 20 MB only.
func jsDelivrURL(name string) string {
	return "https://cdn.jsdelivr.net/gh/lionsoul2014/ip2region@" + Release + "/data/" + name
}

// The data files. The IPv6 one is too large for jsDelivr.
var (
	V4 = DataFile{
		Name:   "ip2region_v4.xdb",
		URLs:   []string{githubURL("ip2region_v4.xdb"), jsDelivrURL("ip2region_v4.xdb")},
		Size:   11122036,
		SHA256: "8e31bbdccb5bf21028af10592d4312ec975da0bffa108c0c5d862a12190f9ad3",
	}
	V6 = DataFile{
		Name:   "ip2region_v6.xdb",
		URLs:   []string{githubURL("ip2region_v6.xdb")},
		Size:   37277813,
		SHA256: "939f6b46bd2b8bec3cf7c5ceb8ba782266ae9b1f35b5ba7916700dec0b7506ed",
	}
)

// userAgent is the User-Agent of the app's requests: ip.sb refuses the
// default ones.
const userAgent = "finallyshell"

// firstByteTimeout is how long a source has to send the first byte of
// the file, connecting included, before the next one is tried. Once the
// file is coming there is no limit: 11 MB on a slow link takes long.
var firstByteTimeout = 15 * time.Second

// ErrNoAnswer is a source that sent nothing within firstByteTimeout.
var ErrNoAnswer = errors.New("no answer in time")

// StatusError is a source that answered with an HTTP status other than
// 200.
type StatusError struct {
	Code   int
	Status string // "404 Not Found"
}

func (e *StatusError) Error() string { return e.Status }

// SourceError is why the download from one source failed.
type SourceError struct {
	Host string
	Err  error
}

func (e *SourceError) Error() string { return e.Host + ": " + e.Err.Error() }
func (e *SourceError) Unwrap() error { return e.Err }

// DownloadError is a download that failed from every source: why, for
// each, in the order they were tried.
type DownloadError struct {
	Sources []*SourceError
}

func (e *DownloadError) Error() string {
	msgs := make([]string, len(e.Sources))
	for i, s := range e.Sources {
		msgs[i] = s.Error()
	}
	return strings.Join(msgs, "; ")
}

func (e *DownloadError) Unwrap() []error {
	errs := make([]error, len(e.Sources))
	for i, s := range e.Sources {
		errs[i] = s
	}
	return errs
}

// Download fetches f into dir with client (http.DefaultClient when nil),
// calling progress, if not nil, with the bytes read so far and the size,
// as they arrive. It tries f's URLs in order, moving to the next when one
// cannot be reached, sends nothing within firstByteTimeout, answers
// anything but 200 or serves a file whose size or SHA-256 is not f's;
// progress starts from 0 again for each. The file is written under a
// temporary name and renamed to f.Name only when its SHA-256 is f's: a
// failed, wrong or canceled download leaves nothing behind.
//
// When every source fails the error is a *DownloadError. When ctx ends
// it is ctx.Err(), and no further source is tried; so too an error
// writing the file, which the next source would meet as well.
func Download(ctx context.Context, client *http.Client, f DataFile, dir string, progress func(done, total int64)) error {
	if client == nil {
		client = http.DefaultClient
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if len(f.URLs) == 0 {
		return errors.New("no source for " + f.Name)
	}
	var failed DownloadError
	for i, url := range f.URLs {
		if i > 0 && progress != nil {
			progress(0, f.Size)
		}
		err := downloadFrom(ctx, client, f, url, dir, progress)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var local *localError
		if errors.As(err, &local) {
			return local.err
		}
		failed.Sources = append(failed.Sources, &SourceError{Host: hostOf(url), Err: err})
	}
	return &failed
}

// localError is a download that failed on this computer, writing the
// file, rather than at its source.
type localError struct{ err error }

func (e *localError) Error() string { return e.err.Error() }

// downloadFrom is Download from one source.
func downloadFrom(ctx context.Context, client *http.Client, f DataFile, url, dir string, progress func(done, total int64)) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	timer := time.AfterFunc(firstByteTimeout, func() { cancel(ErrNoAnswer) })
	defer timer.Stop()
	// noAnswer is err, or ErrNoAnswer when the timer is what ended it.
	noAnswer := func(err error) error {
		if errors.Is(context.Cause(ctx), ErrNoAnswer) {
			return ErrNoAnswer
		}
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return noAnswer(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &StatusError{Code: resp.StatusCode, Status: resp.Status}
	}
	// A file whose length the source says is not f's is not f: do not
	// read it.
	if f.Size > 0 && resp.ContentLength >= 0 && resp.ContentLength != f.Size {
		return ErrChecksum
	}
	total := f.Size
	if total <= 0 {
		total = resp.ContentLength
	}
	tmp, err := os.CreateTemp(dir, f.Name+".*.part")
	if err != nil {
		return &localError{err}
	}
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	h := sha256.New()
	// A file larger than f is not f: stop reading a little past its size.
	limit := int64(1 << 40)
	if f.Size > 0 {
		limit = f.Size + 1
	}
	body := &firstByteReader{r: io.LimitReader(resp.Body, limit), timer: timer}
	w := &progressWriter{w: io.MultiWriter(tmp, h), total: total, progress: progress}
	n, err := io.Copy(w, body)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		if w.err != nil {
			return &localError{w.err}
		}
		return noAnswer(err)
	}
	if f.Size > 0 && n != f.Size || f.SHA256 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), f.SHA256) {
		return ErrChecksum
	}
	if err := tmp.Sync(); err != nil {
		return &localError{err}
	}
	if err := tmp.Close(); err != nil {
		return &localError{err}
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, f.Name)); err != nil {
		return &localError{err}
	}
	done = true
	return nil
}

// firstByteReader stops timer when the first byte arrives.
type firstByteReader struct {
	r     io.Reader
	timer *time.Timer
	got   bool
}

func (f *firstByteReader) Read(b []byte) (int, error) {
	n, err := f.r.Read(b)
	if n > 0 && !f.got {
		f.got = true
		f.timer.Stop()
	}
	return n, err
}

// CleanParts removes what downloads left in dir when the app ended
// during them.
func CleanParts(dir string) {
	parts, _ := filepath.Glob(filepath.Join(dir, "*.part"))
	for _, p := range parts {
		os.Remove(p)
	}
}

type progressWriter struct {
	w        io.Writer
	done     int64
	total    int64
	progress func(done, total int64)
	last     time.Time
	err      error // the error writing, rather than reading
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	if err != nil {
		p.err = err
	}
	p.done += int64(n)
	// At most 20 calls a second, and one at the end.
	if p.progress != nil && (time.Since(p.last) >= 50*time.Millisecond || p.done >= p.total) {
		p.last = time.Now()
		p.progress(p.done, p.total)
	}
	return n, err
}

func hostOf(url string) string {
	_, rest, _ := strings.Cut(url, "://")
	host, _, _ := strings.Cut(rest, "/")
	return host
}
