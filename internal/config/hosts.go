package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// HostsVersion is the version of hosts.json this build writes.
//
// Version 1 was a bare JSON list of hosts. Version 2 is an object:
//
//	{"version": 2, "folders": [...], "hosts": [...]}
const HostsVersion = 2

// AuthKind is how a host logs in.
type AuthKind string

const (
	AuthPassword AuthKind = "password"
	AuthKey      AuthKind = "key"
)

// Host is a saved connection. Passwords are never saved here: the app
// keeps them in the system's credential store, or in memory.
//
// New fields (a proxy, tunnels, terminal options) go at the end, with
// omitempty or omitzero, so files without them load as before. JSON keys
// this build does not know are kept and written back on save.
type Host struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Addr    string   `json:"addr"`
	Port    int      `json:"port,omitempty"`
	User    string   `json:"user"`
	Auth    AuthKind `json:"auth"`
	KeyPath string   `json:"keyPath,omitempty"`

	// Folder is the ID of the folder the host is in, "" for the top level.
	Folder string `json:"folder,omitempty"`
	// Notes are the user's own words about the host.
	Notes string `json:"notes,omitempty"`
	// NoMonitor turns off the side panel's polling, which opens an exec
	// channel every few seconds; some jump hosts drop extra channels. It
	// is the inverse of FinalShell's "Enable exec channel", so that a
	// host without the key, as every host before version 2, and a new
	// Host{} both have monitoring on. Read it with MonitorEnabled.
	NoMonitor bool `json:"noMonitor,omitempty"`
	// LastConnectedAt is when a session to the host last connected; zero
	// for never, or since the recent list was cleared.
	LastConnectedAt time.Time `json:"lastConnectedAt,omitzero"`
	// DeletedAt is when the host was moved to the deleted hosts; zero for
	// a host that is not deleted.
	DeletedAt time.Time `json:"deletedAt,omitzero"`

	// TermType is the terminal type the shell is asked for, its TERM: ""
	// for DefaultTermType. Read it with TermTypeOrDefault.
	TermType string `json:"termType,omitempty"`
	// Backspace is what the Backspace key sends: "" for ^? (DEL), as
	// most terminals send, or BackspaceCtrlH.
	Backspace BackspaceKey `json:"backspace,omitempty"`
	// NoRememberPassword keeps the host's password, or its key's
	// passphrase, out of the system's credential store: the app keeps it
	// in memory until it quits. It is the inverse of "Remember password",
	// so that a host without the key remembers it. Read it with
	// RemembersPassword.
	NoRememberPassword bool `json:"noRememberPassword,omitempty"`

	// Proxy is how the host is reached: through a SOCKS5 or HTTP proxy,
	// or through another saved host, a jump host. nil connects directly.
	// Its password is not saved, as the host's is not.
	Proxy *ProxyConfig `json:"proxy,omitempty"`

	// Tunnels are the ports forwarded through the host's connection, as
	// ssh -L, -R and -D: each enabled one starts as a session connects.
	Tunnels []Tunnel `json:"tunnels,omitempty"`

	extra extras
}

// DefaultTermType is the terminal type of a host that names none: what
// the terminal is, Ghostty's VT core with 256 colors and more.
const DefaultTermType = "xterm-256color"

// BackspaceKey is what the Backspace key sends to a host.
type BackspaceKey string

const (
	// BackspaceDEL sends ^? (0x7f), as xterm, Ghostty and most others.
	BackspaceDEL BackspaceKey = ""
	// BackspaceCtrlH sends ^H (0x08), for servers whose erase character
	// is ^H, as some old systems and serial consoles.
	BackspaceCtrlH BackspaceKey = "ctrl-h"
)

// PortOr22 is the host's port, 22 when unset.
func (h *Host) PortOr22() int {
	if h.Port == 0 {
		return 22
	}
	return h.Port
}

// Title is the host's name, else its address.
func (h *Host) Title() string {
	if h.Name != "" {
		return h.Name
	}
	return h.Addr
}

// Target is user@addr, with the port when it is not 22.
func (h *Host) Target() string {
	s := h.User + "@" + h.Addr
	if h.PortOr22() != 22 {
		s += ":" + strconv.Itoa(h.PortOr22())
	}
	return s
}

// TermTypeOrDefault is the host's terminal type, DefaultTermType when
// unset.
func (h *Host) TermTypeOrDefault() string {
	if h.TermType == "" {
		return DefaultTermType
	}
	return h.TermType
}

// MonitorEnabled says whether sessions to h poll the server for the side
// panel.
func (h *Host) MonitorEnabled() bool { return !h.NoMonitor }

// RemembersPassword says whether the app keeps h's password, or its
// key's passphrase, in the system's credential store.
func (h *Host) RemembersPassword() bool { return !h.NoRememberPassword }

// Clone is a copy of h, its proxy, its tunnels and the keys this build
// does not know included, sharing nothing with h, with the same ID: give
// it another before adding it.
func (h *Host) Clone() *Host {
	c := *h
	c.extra = maps.Clone(h.extra)
	if h.Proxy != nil {
		p := *h.Proxy
		p.extra = maps.Clone(h.Proxy.extra)
		c.Proxy = &p
	}
	c.Tunnels = slices.Clone(h.Tunnels)
	for i := range c.Tunnels {
		c.Tunnels[i].extra = maps.Clone(h.Tunnels[i].extra)
	}
	return &c
}

// Deleted says whether h is soft-deleted.
func (h *Host) Deleted() bool { return !h.DeletedAt.IsZero() }

type hostJSON Host

var hostKeys = knownKeys(reflect.TypeFor[hostJSON]())

func (h Host) MarshalJSON() ([]byte, error) { return encodeObject(hostJSON(h), h.extra) }

func (h *Host) UnmarshalJSON(data []byte) error {
	var v hostJSON
	ex, err := decodeObject(data, &v, hostKeys)
	if err != nil {
		return err
	}
	*h = Host(v)
	h.extra = ex
	return nil
}

// NewHostID returns an ID for a new host.
func NewHostID() string { return fmt.Sprintf("h%d", time.Now().UnixNano()) }

// Folder groups hosts. Folders nest by Parent.
type Folder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Parent is the ID of the folder this one is in, "" for the top level.
	Parent string `json:"parent,omitempty"`

	extra extras
}

type folderJSON Folder

var folderKeys = knownKeys(reflect.TypeFor[folderJSON]())

func (f Folder) MarshalJSON() ([]byte, error) { return encodeObject(folderJSON(f), f.extra) }

func (f *Folder) UnmarshalJSON(data []byte) error {
	var v folderJSON
	ex, err := decodeObject(data, &v, folderKeys)
	if err != nil {
		return err
	}
	*f = Folder(v)
	f.extra = ex
	return nil
}

// NewFolderID returns an ID for a new folder.
func NewFolderID() string { return fmt.Sprintf("f%d", time.Now().UnixNano()) }

// Store is the content of hosts.json: the saved hosts, deleted ones
// included, and their folders, both in the order the user keeps them.
type Store struct {
	// Version is the file's version: HostsVersion, or more for a file a
	// newer build wrote, which saving keeps.
	Version int       `json:"version"`
	Folders []*Folder `json:"folders"`
	Hosts   []*Host   `json:"hosts"`

	extra extras
}

type storeJSON Store

var storeKeys = knownKeys(reflect.TypeFor[storeJSON]())

func (s Store) MarshalJSON() ([]byte, error) {
	v := storeJSON(s)
	v.Version = max(v.Version, HostsVersion)
	if v.Folders == nil {
		v.Folders = []*Folder{}
	}
	if v.Hosts == nil {
		v.Hosts = []*Host{}
	}
	return encodeObject(v, s.extra)
}

func (s *Store) UnmarshalJSON(data []byte) error {
	var v storeJSON
	ex, err := decodeObject(data, &v, storeKeys)
	if err != nil {
		return err
	}
	*s = Store(v)
	s.extra = ex
	s.Folders = dropNil(s.Folders)
	s.Hosts = dropNil(s.Hosts)
	if s.Version == 0 {
		s.Version = HostsVersion
	}
	return nil
}

// dropNil removes the nulls of a JSON list, nil when none is left.
func dropNil[T any](items []*T) []*T {
	items = slices.DeleteFunc(items, func(it *T) bool { return it == nil })
	if len(items) == 0 {
		return nil
	}
	return items
}

// NewStore is an empty store.
func NewStore() *Store { return &Store{Version: HostsVersion} }

// ParseHosts reads the content of hosts.json, of any version. migrated
// is true for a version 1 file, a bare list of hosts: each keeps all its
// keys, has no folder and has monitoring on.
func ParseHosts(data []byte) (s *Store, migrated bool, err error) {
	data = bytes.TrimSpace(data)
	switch {
	case len(data) == 0 || string(data) == "null":
		return NewStore(), false, nil
	case data[0] == '[':
		var hosts []*Host
		if err := json.Unmarshal(data, &hosts); err != nil {
			return nil, false, err
		}
		s = NewStore()
		s.Hosts = dropNil(hosts)
		return s, true, nil
	}
	s = &Store{}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, false, err
	}
	return s, false, nil
}

func hostsPath() string { return filepath.Join(Dir(), "hosts.json") }

// V1BackupName is the copy of a version 1 hosts.json that LoadHosts
// keeps when it migrates one, as older builds cannot read version 2.
const V1BackupName = "hosts.v1.json"

// now is the clock that names the copies of broken files; tests set it.
var now = time.Now

// BrokenName is the copy LoadHosts makes, next to hosts.json, of a
// hosts.json it cannot parse, at t: hosts.json.broken-20061231-235959.
func BrokenName(t time.Time) string { return "hosts.json.broken-" + t.Format("20060102-150405") }

// LoadHosts reads hosts.json: an empty store when there is no file yet,
// or with an error. A version 1 file is migrated (see ParseHosts) and,
// the first time, copied to V1BackupName; it is rewritten as version 2
// at the next SaveHosts. A file that does not parse is copied to
// BrokenName first, as the next SaveHosts replaces it.
func LoadHosts() (*Store, error) {
	data, err := os.ReadFile(hostsPath())
	if os.IsNotExist(err) {
		return NewStore(), nil
	}
	if err != nil {
		return NewStore(), err
	}
	s, migrated, err := ParseHosts(data)
	if err != nil {
		broken := filepath.Join(Dir(), BrokenName(now()))
		if werr := writeFileAtomic(broken, data); werr != nil { // best effort
			return NewStore(), fmt.Errorf("%s: %w", hostsPath(), err)
		}
		return NewStore(), fmt.Errorf("%s: %w (copied to %s)", hostsPath(), err, broken)
	}
	if migrated {
		backup := filepath.Join(Dir(), V1BackupName)
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			writeFileAtomic(backup, data) // best effort: the hosts loaded
		}
	}
	return s, nil
}

// SaveHosts writes s as hosts.json, replacing the file at once.
func SaveHosts(s *Store) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(hostsPath(), append(data, '\n'))
}

// writeFileAtomic writes data to a temporary file next to path and
// renames it over path, so that a crash leaves the old file or the new.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// Host is the host with the ID, deleted or not; nil when there is none.
func (s *Store) Host(id string) *Host {
	for _, h := range s.Hosts {
		if h.ID == id {
			return h
		}
	}
	return nil
}

// Add appends h, giving it an ID when it has none.
func (s *Store) Add(h *Host) {
	if h.ID == "" {
		h.ID = NewHostID()
	}
	s.Hosts = append(s.Hosts, h)
}

// Live are the hosts that are not deleted, in order.
func (s *Store) Live() []*Host {
	return slices.DeleteFunc(slices.Clone(s.Hosts), (*Host).Deleted)
}

// Deleted are the soft-deleted hosts, the last deleted first.
func (s *Store) Deleted() []*Host {
	hosts := slices.DeleteFunc(slices.Clone(s.Hosts), func(h *Host) bool { return !h.Deleted() })
	slices.SortStableFunc(hosts, func(a, b *Host) int { return b.DeletedAt.Compare(a.DeletedAt) })
	return hosts
}

// Recent are the hosts that are not deleted and have connected since the
// recent list was last cleared, the last connected first: at most n, or
// all for n <= 0.
func (s *Store) Recent(n int) []*Host {
	hosts := slices.DeleteFunc(slices.Clone(s.Hosts), func(h *Host) bool { return h.Deleted() || h.LastConnectedAt.IsZero() })
	slices.SortStableFunc(hosts, func(a, b *Host) int { return b.LastConnectedAt.Compare(a.LastConnectedAt) })
	if n > 0 && len(hosts) > n {
		hosts = hosts[:n]
	}
	return hosts
}

// MarkConnected notes that the host with the ID connected at t. It
// reports whether there is such a host.
func (s *Store) MarkConnected(id string, t time.Time) bool {
	h := s.Host(id)
	if h != nil {
		h.LastConnectedAt = t
	}
	return h != nil
}

// ClearRecents empties the recent list, keeping the hosts.
func (s *Store) ClearRecents() {
	for _, h := range s.Hosts {
		h.LastConnectedAt = time.Time{}
	}
}

// SoftDelete moves the host with the ID to the deleted hosts, as of t,
// unless it is there already. It reports whether there is such a host.
func (s *Store) SoftDelete(id string, t time.Time) bool {
	h := s.Host(id)
	if h != nil && !h.Deleted() {
		h.DeletedAt = t
	}
	return h != nil
}

// Restore brings back the deleted host with the ID. It reports whether
// there is such a host.
func (s *Store) Restore(id string) bool {
	h := s.Host(id)
	if h != nil {
		h.DeletedAt = time.Time{}
	}
	return h != nil
}

// Remove deletes the host with the ID for good. It reports whether there
// was such a host.
func (s *Store) Remove(id string) bool {
	n := len(s.Hosts)
	s.Hosts = slices.DeleteFunc(s.Hosts, func(h *Host) bool { return h.ID == id })
	return len(s.Hosts) < n
}

// Folder is the folder with the ID; nil for "" or when there is none.
func (s *Store) Folder(id string) *Folder {
	if id == "" {
		return nil
	}
	for _, f := range s.Folders {
		if f.ID == id {
			return f
		}
	}
	return nil
}

// parentOf is the ID of the folder f is shown in: its Parent, or "" (the
// top level) when that is "", missing, or f itself by way of its parents.
func (s *Store) parentOf(f *Folder) string {
	if s.Folder(f.Parent) == nil {
		return ""
	}
	cur := f.Parent
	for range len(s.Folders) {
		if cur == f.ID {
			return ""
		}
		p := s.Folder(cur)
		if p == nil {
			break
		}
		cur = p.Parent
	}
	return f.Parent
}

// folderOf is the ID of the folder h is shown in: "" when its folder is
// missing.
func (s *Store) folderOf(h *Host) string {
	if s.Folder(h.Folder) == nil {
		return ""
	}
	return h.Folder
}

// Subfolders are the folders directly in the folder with the ID, "" for
// the top level, in order. Folders whose parent is missing, or that are
// inside themselves, are at the top level.
func (s *Store) Subfolders(parent string) []*Folder {
	var out []*Folder
	for _, f := range s.Folders {
		if s.parentOf(f) == parent {
			out = append(out, f)
		}
	}
	return out
}

// HostsIn are the hosts directly in the folder with the ID, "" for the
// top level, that are not deleted, in order. Hosts whose folder is
// missing are at the top level.
func (s *Store) HostsIn(folder string) []*Host {
	var out []*Host
	for _, h := range s.Hosts {
		if !h.Deleted() && s.folderOf(h) == folder {
			out = append(out, h)
		}
	}
	return out
}

// FolderPath is the folder with the ID and the folders it is in, the
// top-level one first: nil when there is no such folder.
func (s *Store) FolderPath(id string) []*Folder {
	var path []*Folder
	for f := s.Folder(id); f != nil && len(path) <= len(s.Folders); f = s.Folder(s.parentOf(f)) {
		path = append(path, f)
	}
	slices.Reverse(path)
	return path
}

// AddFolder makes a folder named name in the folder parent, "" for the
// top level.
func (s *Store) AddFolder(name, parent string) *Folder {
	f := &Folder{ID: NewFolderID(), Name: name, Parent: parent}
	s.Folders = append(s.Folders, f)
	return f
}

// RemoveFolder deletes the folder with the ID, moving its folders and
// hosts, deleted ones included, to the folder it was in. It reports
// whether there was such a folder.
func (s *Store) RemoveFolder(id string) bool {
	f := s.Folder(id)
	if f == nil {
		return false
	}
	parent := s.parentOf(f)
	for _, sub := range s.Folders {
		if sub.Parent == id {
			sub.Parent = parent
		}
	}
	for _, h := range s.Hosts {
		if h.Folder == id {
			h.Folder = parent
		}
	}
	s.Folders = slices.DeleteFunc(s.Folders, func(g *Folder) bool { return g == f })
	return true
}

// extras are the keys of a JSON object that this build does not know,
// kept as they were so that saving does not drop what a newer build
// wrote.
type extras map[string]json.RawMessage

// knownKeys are the JSON names of the fields of t, a struct, in lower
// case: encoding/json matches names regardless of case.
func knownKeys(t reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		keys[strings.ToLower(name)] = true
	}
	return keys
}

// decodeObject decodes the JSON object data into v and returns the keys
// that are not in known.
func decodeObject(data []byte, v any, known map[string]bool) (extras, error) {
	if err := json.Unmarshal(data, v); err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	var ex extras
	for k, raw := range all {
		if !known[strings.ToLower(k)] {
			if ex == nil {
				ex = extras{}
			}
			var b bytes.Buffer
			json.Compact(&b, raw) // valid: it was decoded
			ex[k] = b.Bytes()
		}
	}
	return ex, nil
}

// encodeObject encodes v, a struct, as a JSON object followed by the
// extra keys.
func encodeObject(v any, ex extras) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil || len(ex) == 0 {
		return data, err
	}
	var b bytes.Buffer
	b.Write(data[:len(data)-1]) // without the closing brace
	first := len(data) == 2     // {}
	for _, k := range slices.Sorted(maps.Keys(ex)) {
		if !first {
			b.WriteByte(',')
		}
		first = false
		key, _ := json.Marshal(k)
		b.Write(key)
		b.WriteByte(':')
		b.Write(ex[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
