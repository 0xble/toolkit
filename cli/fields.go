package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/0xble/toolkit/op"
)

// filterFields applies --fields to an operation's output. Without Paged it
// keeps the named top-level keys, as output.FilterFields does. A Paged output
// keeps the named keys of each element of its items array and every other
// top-level key, since dropping the envelope would hide which account
// answered and whether more pages exist. Numbers pass through as json.Number,
// so an integer above 2^53 prints exactly.
func filterFields(e *op.Entry, v any, fields []string) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal for field filtering: %w", err)
	}
	if !e.Paged {
		return pickFields(b, fields)
	}
	var page map[string]any
	if err := decodeNumbers(b, &page); err != nil || page == nil {
		return nil, fmt.Errorf("fields filter on a page requires an object with an items array")
	}
	if items, ok := page["items"]; ok && items != nil {
		if b, err = json.Marshal(items); err != nil {
			return nil, fmt.Errorf("marshal for field filtering: %w", err)
		}
		if page["items"], err = pickFields(b, fields); err != nil {
			return nil, err
		}
	}
	return page, nil
}

// pickFields keeps the named keys of a JSON object or of each object in a
// JSON array, as output.FilterFields does.
func pickFields(data []byte, fields []string) (any, error) {
	var arr []map[string]any
	if err := decodeNumbers(data, &arr); err == nil {
		result := make([]map[string]any, len(arr))
		for i, m := range arr {
			result[i] = pickKeys(m, fields)
		}
		return result, nil
	}
	var obj map[string]any
	if err := decodeNumbers(data, &obj); err != nil {
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

func decodeNumbers(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}
