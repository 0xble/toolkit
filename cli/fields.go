package cli

import (
	"encoding/json"
	"fmt"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/output"
)

// filterFields applies --fields to an operation's output. Without Paged it
// keeps the named top-level keys, as output.FilterFields does. A Paged output
// keeps the named keys of each element of its items array and every other
// top-level key, since dropping the envelope would hide which account
// answered and whether more pages exist.
func filterFields(e *op.Entry, v any, fields []string) (any, error) {
	if !e.Paged {
		return output.FilterFields(v, fields)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal for field filtering: %w", err)
	}
	var page map[string]any
	if err := json.Unmarshal(b, &page); err != nil || page == nil {
		return nil, fmt.Errorf("fields filter on a page requires an object with an items array")
	}
	if items, ok := page["items"]; ok && items != nil {
		if page["items"], err = output.FilterFields(items, fields); err != nil {
			return nil, err
		}
	}
	return page, nil
}
