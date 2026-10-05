package cli_test

import (
	"context"
	"testing"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

type meeting struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type meetingsPage struct {
	Account    string    `json:"account"`
	Items      []meeting `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
	HasMore    bool      `json:"has_more"`
}

func pages(paged bool, items []meeting) *op.Registry {
	r := op.New("t", "v1")
	op.Add(r, op.Op[struct{}, meetingsPage]{Name: "meetings", Effect: op.Read, Paged: paged,
		Handler: func(context.Context, op.Request, struct{}) (meetingsPage, error) {
			return meetingsPage{Account: "work", Items: items, NextCursor: "c2", HasMore: true}, nil
		},
	})
	return r
}

func TestPagedFields(t *testing.T) {
	items := []meeting{{ID: "m1", Title: "Standup", URL: "u1"}, {ID: "m2", Title: "Review", URL: "u2"}}
	for _, tc := range []struct {
		name   string
		paged  bool
		items  []meeting
		fields string
		want   string
	}{
		{"paged projects each item", true, items, "id,title",
			`{"account":"work","items":[{"id":"m1","title":"Standup"},{"id":"m2","title":"Review"}],"next_cursor":"c2","has_more":true}`},
		{"paged keeps the envelope when an envelope key is named", true, items, "title,has_more",
			`{"account":"work","items":[{"title":"Standup"},{"title":"Review"}],"next_cursor":"c2","has_more":true}`},
		{"paged empty page", true, []meeting{}, "id",
			`{"account":"work","items":[],"next_cursor":"c2","has_more":true}`},
		{"paged null items stay null", true, nil, "id",
			`{"account":"work","items":null,"next_cursor":"c2","has_more":true}`},
		{"unpaged keeps top-level keys", false, items, "id,title", `{}`},
		{"unpaged names an envelope key", false, items, "next_cursor,has_more", `{"next_cursor":"c2","has_more":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, stderr := run(t, pages(tc.paged, tc.items), "meetings", "--agent", "--fields", tc.fields)
			if code != 0 || !toolkittest.SameJSON([]byte(out), []byte(tc.want)) {
				t.Errorf("exit %d\ngot  %s\nwant %s\n%s", code, out, tc.want, stderr)
			}
		})
	}
}
