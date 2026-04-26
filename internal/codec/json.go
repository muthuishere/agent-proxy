package codec

import (
	"encoding/json"
)

// WalkJSONStrings calls fn for each string leaf value in the JSON tree.
// fn receives the decoded string and returns the replacement string.
// If fn returns the same string, the node is unchanged.
// Returns the re-serialised JSON, whether any node changed, and any error.
// On error the original data is returned unchanged.
func WalkJSONStrings(data []byte, fn func(s string) string) ([]byte, bool, error) {
	var root interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return data, false, err
	}
	changed := walkValue(&root, fn)
	if !changed {
		return data, false, nil
	}
	out, err := json.Marshal(root)
	if err != nil {
		return data, false, err
	}
	return out, true, nil
}

func walkValue(v *interface{}, fn func(s string) string) bool {
	if v == nil || *v == nil {
		return false
	}
	switch val := (*v).(type) {
	case string:
		replaced := fn(val)
		if replaced != val {
			*v = replaced
			return true
		}
		return false
	case map[string]interface{}:
		changed := false
		for k, child := range val {
			if walkValue(&child, fn) {
				val[k] = child
				changed = true
			}
		}
		return changed
	case []interface{}:
		changed := false
		for i := range val {
			if walkValue(&val[i], fn) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}
