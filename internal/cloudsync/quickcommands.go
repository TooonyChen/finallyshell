package cloudsync

import (
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
		if !localMoved && (remoteMoved || slices.Equal(itemIDs(lists[0]), itemIDs(lists[1]))) {
			// Remote order wins when this device did not reorder. Keep
			// local additions too, after the remote's surviving items.
			remaining := byID(items)
			ordered := make([]json.RawMessage, 0, len(items))
			for _, item := range lists[2] {
				if id := idOf(item); remaining[id] != nil {
					ordered = append(ordered, remaining[id])
					delete(remaining, id)
				}
			}
			for _, item := range items {
				if id := idOf(item); id == "" || remaining[id] != nil {
					ordered = append(ordered, item)
					delete(remaining, id)
				}
			}
			items = ordered
		}
		out[key], _ = json.Marshal(items)
	}
	data, _ := json.Marshal(out)
	return data, conflicts
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
