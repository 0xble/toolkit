package output

import (
	"encoding/json"
	"fmt"
	"strings"
)

func FilterFields(v any, fields []string) (any, error) {
	if len(fields) == 0 {
		return v, nil
	}

	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal for field filtering: %w", err)
	}

	// Try as array first
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err == nil {
		result := make([]map[string]any, len(arr))
		for i, m := range arr {
			result[i] = pickKeys(m, fields)
		}
		return result, nil
	}

	// Try as single object
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("fields filter requires object or array of objects")
	}
	return pickKeys(obj, fields), nil
}

func pickKeys(m map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}
