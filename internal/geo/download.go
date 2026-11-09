package geo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DataFile is an ip2region data file the app downloads: where from, its
// size and its SHA-256, which the app keeps so that only that file is
// ever used.
type DataFile struct {
	Name   string // the file's name in the data folder
	URL    string
	Size   int64
	SHA256 string // hex
}

// The data files, from ip2region's release v3.18.0 on GitHub
// (https://github.com/lionsoul2014/ip2region/tree/v3.18.0/data). A newer
// release is a change of these lines: its URLs, sizes and checksums.
var (
	V4 = DataFile{
		Name:   "ip2region_v4.xdb",
		URL:    "https://raw.githubusercontent.com/lionsoul2014/ip2region/v3.18.0/data/ip2region_v4.xdb",
		Size:   11122036,
		SHA256: "8e31bbdccb5bf21028af10592d4312ec975da0bffa108c0c5d862a12190f9ad3",
	}
	V6 = DataFile{
		Name:   "ip2region_v6.xdb",
		URL:    "https://raw.githubusercontent.com/lionsoul2014/ip2region/v3.18.0/data/ip2region_v6.xdb",
		Size:   37277813,
		SHA256: "939f6b46bd2b8bec3cf7c5ceb8ba782266ae9b1f35b5ba7916700dec0b7506ed",
	}
)

// userAgent is the User-Agent of the app's requests: ip.sb refuses the
// default ones.
const userAgent = "finallyshell"

// Download fetches f into dir with client (http.DefaultClient when nil),
// calling progress, if not nil, with the bytes read so far and the size,
// as they arrive. The file is written under a temporary name and renamed
// to f.Name only when its SHA-256 is f's: a failed, wrong or canceled
// download leaves nothing behind.
func Download(ctx context.Context, client *http.Client, f DataFile, dir string, progress func(done, total int64)) error {
	if client == nil {
		client = http.DefaultClient
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", hostOf(f.URL), resp.Status)
	}
	total := f.Size
	if total <= 0 {
		total = resp.ContentLength
	}
	tmp, err := os.CreateTemp(dir, f.Name+".*.part")
	if err != nil {
		return err
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
	w := &progressWriter{w: io.MultiWriter(tmp, h), total: total, progress: progress}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit))
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return err
	}
	if f.Size > 0 && n != f.Size || f.SHA256 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), f.SHA256) {
		return ErrChecksum
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, f.Name)); err != nil {
		return err
	}
	done = true
	return nil
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
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
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
