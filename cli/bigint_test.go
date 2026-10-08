package cli_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/0xble/toolkit/op"
)

type bigIn struct {
	ID    int64   `json:"id" arg:"" help:"ID"`
	Max   uint64  `json:"max" arg:"" help:"Max"`
	Ref   int64   `json:"ref,omitempty" help:"Ref"`
	Cap   uint64  `json:"cap,omitempty" help:"Cap"`
	Ratio float64 `json:"ratio,omitempty" help:"Ratio"`
}

type bigItem struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type bigPage struct {
	Items []bigItem `json:"items"`
	Next  uint64    `json:"next"`
}

func bigRegistry() *op.Registry {
	r := op.New("t", "v1")
	op.Add(r, op.Op[bigIn, bigIn]{Name: "big", Effect: op.Read,
		Handler: func(_ context.Context, _ op.Request, in bigIn) (bigIn, error) { return in, nil }})
	op.Add(r, op.Op[struct{}, bigPage]{Name: "bigs", Effect: op.Read, Paged: true,
		Handler: func(context.Context, op.Request, struct{}) (bigPage, error) {
			return bigPage{Items: []bigItem{{ID: 5312241539987020022, Name: "a"}}, Next: 18446744073709551615}, nil
		}})
	return r
}

// TestIntegersAbove2To53 checks that an int64 or uint64 above 2^53 given as a
// positional argument or a flag reaches the handler exactly, and that the
// output prints exactly with --json and --fields.
func TestIntegersAbove2To53(t *testing.T) {
	const id, max = "5312241539987020022", "18446744073709551615"
	in := []string{"big", id, max, "--ref", id, "--cap", max, "--ratio", "1.5"}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{append(in, "--json"), `{"id":` + id + `,"max":` + max + `,"ref":` + id + `,"cap":` + max + `,"ratio":1.5}`},
		{append(in, "--json", "--fields", "id,cap"), `{"id":` + id + `,"cap":` + max + `}`},
		{[]string{"bigs", "--json", "--fields", "id"}, `{"items":[{"id":` + id + `}],"next":` + max + `}`},
	} {
		code, out, stderr := run(t, bigRegistry(), tc.args...)
		if code != 0 || !sameExactJSON(t, out, tc.want) {
			t.Errorf("%q: exit %d\ngot  %s\nwant %s\n%s", tc.args, code, out, tc.want, stderr)
		}
	}
	code, _, stderr := run(t, bigRegistry(), "big", "x", max, "--json")
	if code == 0 {
		t.Errorf("a word for an int64 positional must fail: %s", stderr)
	}
}

// sameExactJSON compares two JSON documents with numbers as written, since
// toolkittest.SameJSON compares them as float64.
func sameExactJSON(t *testing.T, a, b string) bool {
	t.Helper()
	decode := func(s string) any {
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("%v: %s", err, s)
		}
		return v
	}
	return reflect.DeepEqual(decode(a), decode(b))
}
