package store

import "encoding/json"

// ReadList reads a JSON array stored under key into a slice of raw messages.
func (s *Store) ReadList(key string) []json.RawMessage {
	var out []json.RawMessage
	s.ReadInto(key, &out)
	if out == nil {
		out = []json.RawMessage{}
	}
	return out
}

// ReadMapSlice reads a JSON array into a []map[string]interface{}.
func (s *Store) ReadMapSlice(key string) []map[string]interface{} {
	var out []map[string]interface{}
	s.ReadInto(key, &out)
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out
}

// ReadMap reads a JSON object into a map[string]interface{}.
func (s *Store) ReadMap(key string) map[string]interface{} {
	var out map[string]interface{}
	s.ReadInto(key, &out)
	if out == nil {
		out = map[string]interface{}{}
	}
	return out
}

// FindByName returns the first item in a []map where item["name"] == name.
func FindByName(list []map[string]interface{}, name string) (map[string]interface{}, int) {
	for i, item := range list {
		if n, ok := item["name"].(string); ok && n == name {
			return item, i
		}
	}
	return nil, -1
}

// DeleteByName removes the item with the given name from the list.
func DeleteByName(list []map[string]interface{}, name string) []map[string]interface{} {
	_, idx := FindByName(list, name)
	if idx == -1 {
		return list
	}
	return append(list[:idx], list[idx+1:]...)
}

// UpdateByName replaces the item with the given name.
func UpdateByName(list []map[string]interface{}, name string, newItem map[string]interface{}) []map[string]interface{} {
	_, idx := FindByName(list, name)
	if idx == -1 {
		return list
	}
	list[idx] = newItem
	return list
}

// InsertByPosition inserts item at "top" or "bottom" (default).
func InsertByPosition(list []map[string]interface{}, item map[string]interface{}, position string) []map[string]interface{} {
	if position == "top" {
		return append([]map[string]interface{}{item}, list...)
	}
	return append(list, item)
}
