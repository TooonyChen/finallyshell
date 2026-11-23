package cloudsync

import (
	"container/list"
	"encoding/json"
	"maps"
	"slices"
)

// mergeQuickCommands merges each group and command by ID, retaining
// unknown document fields. A missing document (an older schema, or an
// unreadable local file) is not a deletion of all saved commands.
func mergeQuickCommands(base, local, remote json.RawMessage) (json.RawMessage, int) {
	if len(local) == 0 {
		return slices.Clone(remote), 0
	}
	if len(remote) == 0 {
		return slices.Clone(local), 0
	}
	objects := make([]map[string]json.RawMessage, 3)
	for i, raw := range []json.RawMessage{base, local, remote} {
		if len(raw) != 0 {
			if err := json.Unmarshal(raw, &objects[i]); err != nil {
				return slices.Clone(local), 0
			}
		}
	}
	var metadata [3]map[string]json.RawMessage
	for i, obj := range objects {
		metadata[i] = maps.Clone(obj)
		delete(metadata[i], "groups")
		delete(metadata[i], "commands")
	}
	out, conflicts := mergeMap(metadata[0], metadata[1], metadata[2], sameJSON)
	for _, key := range []string{"groups", "commands"} {
		var lists [3][]json.RawMessage
		for i, obj := range objects {
			if raw := obj[key]; len(raw) != 0 {
				if err := json.Unmarshal(raw, &lists[i]); err != nil {
					return slices.Clone(local), 0
				}
			}
		}
		items, n := mergeItems(lists[0], lists[1], lists[2])
		conflicts += n
		localMoved, remoteMoved := orderChanged(lists[0], lists[1]), orderChanged(lists[0], lists[2])
		if localMoved && remoteMoved && orderChanged(lists[1], lists[2]) {
			conflicts++
		}
		primary, secondary := lists[1], lists[2]
		if !localMoved && remoteMoved {
			primary, secondary = secondary, primary
		}
		items = orderQuickItems(items, primary, secondary)
		out[key], _ = json.Marshal(items)
	}
	data, _ := json.Marshal(out)
	return data, conflicts
}

// orderQuickItems keeps the preferred side's order and places additions
// from the other side next to their surviving neighbors. Appending every
// addition at the end loses where a command was duplicated, even when
// the preferred side only deleted an unrelated command.
func orderQuickItems(items, primary, secondary []json.RawMessage) []json.RawMessage {
	merged := byID(items)
	ordered := list.New()
	nodes := map[string]*list.Element{}
	for _, item := range primary {
		id := idOf(item)
		if merged[id] != nil && nodes[id] == nil {
			nodes[id] = ordered.PushBack(merged[id])
		}
	}
	// For a leading addition, use the next surviving primary item.
	next := make([]*list.Element, len(secondary))
	var neighbor *list.Element
	for i := len(secondary) - 1; i >= 0; i-- {
		next[i] = neighbor
		if node := nodes[idOf(secondary[i])]; node != nil {
			neighbor = node
		}
	}
	var previous *list.Element
	for i, item := range secondary {
		id := idOf(item)
		if node := nodes[id]; node != nil {
			previous = node
			continue
		}
		if merged[id] == nil {
			continue
		}
		switch {
		case previous != nil:
			previous = ordered.InsertAfter(merged[id], previous)
		case next[i] != nil:
			previous = ordered.InsertBefore(merged[id], next[i])
		default:
			previous = ordered.PushBack(merged[id])
		}
		nodes[id] = previous
	}
	out := make([]json.RawMessage, 0, len(items))
	for node := ordered.Front(); node != nil; node = node.Next() {
		out = append(out, node.Value.(json.RawMessage))
	}
	for _, item := range items {
		if idOf(item) == "" {
			out = append(out, item)
		}
	}
	return out
}

func itemIDs(items []json.RawMessage) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, idOf(item))
	}
	return ids
}

// orderChanged compares only IDs present on both sides: adding, deleting
// or editing a command does not by itself count as reordering.
func orderChanged(base, current []json.RawMessage) bool {
	b, c := byID(base), byID(current)
	var before, after []string
	for _, id := range itemIDs(base) {
		if c[id] != nil {
			before = append(before, id)
		}
	}
	for _, id := range itemIDs(current) {
		if b[id] != nil {
			after = append(after, id)
		}
	}
	return !slices.Equal(before, after)
}
