package geo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// The online services. ip-api.com's free service is HTTP only, and for
// non-commercial use: the options say so where it is turned on.
const (
	IPAPIURL = "http://ip-api.com/batch"
	IPSBURL  = "https://api.ip.sb/geoip/"
)

// BatchSize is the most addresses ip-api.com takes in one request.
const BatchSize = 100

// ErrBusy is an online lookup the services refused for now, as too many
// were asked: it is tried again later.
var ErrBusy = errors.New("the location service is busy")

// Online looks addresses up on ip-api.com, 100 in a request, keeping to
// its limit of 15 requests a minute with the counts it sends back, and
// on ip.sb, one at a time, while ip-api.com answers 429. It is safe for
// concurrent use; its zero value uses the real services.
type Online struct {
	Client *http.Client // http.DefaultClient when nil
	IPAPI  string       // IPAPIURL when ""
	IPSB   string       // IPSBURL when ""

	mu        sync.Mutex
	apiUntil  time.Time // no request to ip-api.com before
	sbUntil   time.Time // nor to ip.sb
	sleepFunc func(ctx context.Context, d time.Duration) error
}

// Lang is the language of the names ip-api.com gives for the app's
// language tag: Chinese for zh-Hans, else English. ip.sb has English
// only.
func Lang(tag string) string {
	if tag == "zh-Hans" || tag == "zh-CN" || tag == "zh" {
		return "zh-CN"
	}
	return "en"
}

// Lookup asks where ips are, in lang ("en" or "zh-CN"), waiting as the
// services' limits say. It returns what it learned, an empty Location
// for an address the services know nothing of, and an error, ErrBusy
// when they would not answer for now, with what it learned before.
func (o *Online) Lookup(ctx context.Context, ips []string, lang string) (map[string]Location, error) {
	out := map[string]Location{}
	for len(ips) > 0 {
		n := min(len(ips), BatchSize)
		batch := ips[:n]
		ips = ips[n:]
		got, err := o.ipAPI(ctx, batch, lang)
		if errors.Is(err, ErrBusy) {
			got, err = o.ipSB(ctx, batch)
		}
		for ip, loc := range got {
			out[ip] = loc
		}
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func (o *Online) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return http.DefaultClient
}

func (o *Online) sleep(ctx context.Context, d time.Duration) error {
	if o.sleepFunc != nil {
		return o.sleepFunc(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// waitFor waits until *until, which another lookup may move.
func (o *Online) waitFor(ctx context.Context, until *time.Time) error {
	o.mu.Lock()
	d := time.Until(*until)
	o.mu.Unlock()
	if d <= 0 {
		return nil
	}
	return o.sleep(ctx, d)
}

func (o *Online) holdOff(until *time.Time, d time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if t := time.Now().Add(d); t.After(*until) {
		*until = t
	}
}

// ipAPI asks ip-api.com about up to BatchSize addresses.
func (o *Online) ipAPI(ctx context.Context, ips []string, lang string) (map[string]Location, error) {
	if err := o.waitFor(ctx, &o.apiUntil); err != nil {
		return nil, err
	}
	base := o.IPAPI
	if base == "" {
		base = IPAPIURL
	}
	body, _ := json.Marshal(ips)
	u := base + "?fields=status,message,country,regionName,city,isp,query&lang=" + url.QueryEscape(lang)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := o.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// X-Rl is the requests left in this minute, X-Ttl the seconds until
	// it ends.
	ttl, _ := strconv.Atoi(resp.Header.Get("X-Ttl"))
	if ttl <= 0 {
		ttl = 60
	}
	if rl, err := strconv.Atoi(resp.Header.Get("X-Rl")); err == nil && rl <= 0 {
		o.holdOff(&o.apiUntil, time.Duration(ttl)*time.Second)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		o.holdOff(&o.apiUntil, time.Duration(ttl)*time.Second)
		return nil, ErrBusy
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ip-api.com: %s", resp.Status)
	}
	var answers []struct {
		Status     string `json:"status"`
		Country    string `json:"country"`
		RegionName string `json:"regionName"`
		City       string `json:"city"`
		ISP        string `json:"isp"`
		Query      string `json:"query"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &answers); err != nil {
		return nil, fmt.Errorf("ip-api.com: %v", err)
	}
	out := map[string]Location{}
	for _, a := range answers {
		if a.Query == "" {
			continue
		}
		if a.Status != "success" {
			out[a.Query] = Location{}
			continue
		}
		out[a.Query] = Location{Country: a.Country, Region: a.RegionName, City: a.City, ISP: a.ISP}
	}
	return out, nil
}

// sbInterval keeps to ip.sb's 100 requests a minute.
const sbInterval = 650 * time.Millisecond

// ipSB asks ip.sb about each address, one a request.
func (o *Online) ipSB(ctx context.Context, ips []string) (map[string]Location, error) {
	base := o.IPSB
	if base == "" {
		base = IPSBURL
	}
	out := map[string]Location{}
	for _, ip := range ips {
		if err := o.waitFor(ctx, &o.sbUntil); err != nil {
			return out, err
		}
		o.holdOff(&o.sbUntil, sbInterval)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+url.PathEscape(ip), nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := o.client().Do(req)
		if err != nil {
			return out, err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			return out, err
		}
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			o.holdOff(&o.sbUntil, time.Minute)
			return out, ErrBusy
		case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound:
			out[ip] = Location{} // not an address it knows
			continue
		case resp.StatusCode != http.StatusOK:
			return out, fmt.Errorf("ip.sb: %s", resp.Status)
		}
		var a struct {
			Country      string `json:"country"`
			Region       string `json:"region"`
			City         string `json:"city"`
			ISP          string `json:"isp"`
			Organization string `json:"organization"`
		}
		if err := json.Unmarshal(data, &a); err != nil {
			return out, fmt.Errorf("ip.sb: %v", err)
		}
		isp := a.ISP
		if isp == "" {
			isp = a.Organization
		}
		out[ip] = Location{Country: a.Country, Region: a.Region, City: a.City, ISP: isp}
	}
	return out, nil
}
