package cloudsync

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"time"
)

// PayloadSchema is the version of the payload this build writes. A
// remote file with a newer one is not merged, as this build would drop
// what it does not know.
const PayloadSchema = 1

// Payload is what the envelope encrypts: what is synced. Folders and
// hosts are the objects of hosts.json, as JSON, each with its "id"; this
// package does not need to know their other keys, and keeps the ones it
// does not know.
type Payload struct {
	Schema    int       `json:"schema"`
	Device    string    `json:"device,omitempty"` // the name of the device that wrote it
	UpdatedAt time.Time `json:"updatedAt,omitzero"`

	Folders []json.RawMessage `json:"folders"`
	Hosts   []json.RawMessage `json:"hosts"`
	// Settings are the settings that are synced, by their key in
	// settings.json.
	Settings map[string]json.RawMessage `json:"settings"`
	// Secrets are saved passwords and passphrases, by their key in the
	// system's credential store: only from devices that include them.
	Secrets map[string]string `json:"secrets,omitempty"`
}

// Clone is a copy of p sharing no maps or lists with it.
func (p *Payload) Clone() *Payload {
	c := *p
	c.Folders = slices.Clone(p.Folders)
	c.Hosts = slices.Clone(p.Hosts)
	c.Settings = maps.Clone(p.Settings)
	c.Secrets = maps.Clone(p.Secrets)
	return &c
}

// Equal says whether a and b hold the same folders, hosts, settings and
// secrets. The order of folders and hosts, the device and the time do not
// count.
func Equal(a, b *Payload) bool {
	return sameItems(a.Folders, b.Folders) && sameItems(a.Hosts, b.Hosts) &&
		maps.EqualFunc(a.Settings, b.Settings, sameJSON) && maps.Equal(a.Secrets, b.Secrets)
}

// Merge merges local and remote three ways, against base, the payload
// both last agreed on (nil when there is none). Each folder and host (by
// its ID), each setting and each secret is merged on its own:
//
//   - changed on one side only, that side's is kept;
//   - deleted on one side and unchanged on the other, it is deleted;
//   - changed on both sides, local's is kept: the device syncing later
//     wins;
//   - with no base, nothing is deleted: what either side has is kept,
//     local's when both have it.
//
// It returns how many items changed on both sides differently.
func Merge(base, local, remote *Payload) (merged *Payload, conflicts int) {
	if base == nil {
		base = &Payload{}
	}
	m := &Payload{Schema: PayloadSchema}
	var n int
	m.Folders, n = mergeItems(base.Folders, local.Folders, remote.Folders)
	conflicts += n
	m.Hosts, n = mergeItems(base.Hosts, local.Hosts, remote.Hosts)
	conflicts += n
	m.Settings, n = mergeMap(base.Settings, local.Settings, remote.Settings, sameJSON)
	conflicts += n
	m.Secrets, n = mergeMap(base.Secrets, local.Secrets, remote.Secrets, func(a, b string) bool { return a == b })
	conflicts += n
	if len(m.Secrets) == 0 {
		m.Secrets = nil
	}
	return m, conflicts
}

// pick is the three-way decision for one item: whether to keep it, and
// whether local's is kept (else remote's). inB, inL and inR say where it
// is; the eq functions compare it.
func pick(inB, inL, inR bool, lEqB, rEqB, lEqR func() bool) (keep, useLocal, conflict bool) {
	switch {
	case inL && inR:
		if lEqR() {
			return true, true, false
		}
		if inB && lEqB() {
			return true, false, false
		}
		if inB && rEqB() {
			return true, true, false
		}
		return true, true, inB // with no base, it is a first sync rather than a conflict
	case inL: // deleted on the remote, or new here
		if inB && lEqB() {
			return false, false, false
		}
		return true, true, false
	case inR: // deleted here, or new on the remote
		if inB && rEqB() {
			return false, false, false
		}
		return true, false, false
	}
	return false, false, false
}

func mergeMap[V any](base, local, remote map[string]V, eq func(a, b V) bool) (map[string]V, int) {
	out := map[string]V{}
	conflicts := 0
	keys := map[string]bool{}
	for k := range local {
		keys[k] = true
	}
	for k := range remote {
		keys[k] = true
	}
	for k := range keys {
		b, inB := base[k]
		l, inL := local[k]
		r, inR := remote[k]
		keep, useLocal, conflict := pick(inB, inL, inR,
			func() bool { return eq(l, b) }, func() bool { return eq(r, b) }, func() bool { return eq(l, r) })
		if conflict {
			conflicts++
		}
		if !keep {
			continue
		}
		if useLocal {
			out[k] = l
		} else {
			out[k] = r
		}
	}
	return out, conflicts
}

// mergeItems merges lists of objects by their "id": the result has
// local's order, then what only the remote has, in its order. Objects
// without an ID are kept from local and dropped from the remote.
func mergeItems(base, local, remote []json.RawMessage) ([]json.RawMessage, int) {
	b, r := byID(base), byID(remote)
	l := byID(local)
	out := []json.RawMessage{}
	conflicts := 0
	decide := func(id string) {
		bi, inB := b[id]
		li, inL := l[id]
		ri, inR := r[id]
		keep, useLocal, conflict := pick(inB, inL, inR,
			func() bool { return sameJSON(li, bi) }, func() bool { return sameJSON(ri, bi) }, func() bool { return sameJSON(li, ri) })
		if conflict {
			conflicts++
		}
		if !keep {
			return
		}
		if useLocal {
			out = append(out, li)
		} else {
			out = append(out, ri)
		}
	}
	for _, item := range local {
		id := idOf(item)
		if id == "" {
			out = append(out, item)
			continue
		}
		decide(id)
	}
	for _, item := range remote {
		if id := idOf(item); id != "" && !hasKey(l, id) {
			decide(id)
		}
	}
	return out, conflicts
}

func hasKey(m map[string]json.RawMessage, k string) bool { _, ok := m[k]; return ok }

func idOf(item json.RawMessage) string {
	var v struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(item, &v) != nil {
		return ""
	}
	return v.ID
}

func byID(items []json.RawMessage) map[string]json.RawMessage {
	m := make(map[string]json.RawMessage, len(items))
	for _, it := range items {
		if id := idOf(it); id != "" {
			if _, dup := m[id]; !dup {
				m[id] = it
			}
		}
	}
	return m
}

// sameItems says whether two lists hold the same objects, in any order.
func sameItems(a, b []json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	ca, cb := canonicalAll(a), canonicalAll(b)
	slices.Sort(ca)
	slices.Sort(cb)
	return slices.Equal(ca, cb)
}

func canonicalAll(items []json.RawMessage) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = string(canonical(it))
	}
	return out
}

// sameJSON says whether a and b are the same JSON value, whatever the
// order of their keys and their spacing.
func sameJSON(a, b json.RawMessage) bool { return bytes.Equal(canonical(a), canonical(b)) }

// canonical is v re-encoded with sorted keys and no spacing.
func canonical(v json.RawMessage) []byte {
	var x any
	d := json.NewDecoder(bytes.NewReader(v))
	d.UseNumber()
	if d.Decode(&x) != nil {
		return v
	}
	out, err := json.Marshal(x)
	if err != nil {
		return v
	}
	return out
}
