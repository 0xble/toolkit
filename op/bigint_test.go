package op_test

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/0xble/toolkit/op"
)

type bigIn struct {
	ID    int64   `json:"id,omitempty"`
	Max   uint64  `json:"max,omitempty"`
	Ratio float64 `json:"ratio,omitempty"`
	Name  string  `json:"name,omitempty"`
	IDs   []int64 `json:"ids,omitempty"`
}

func bigEntry() *op.Entry {
	r := op.New("t", "v")
	op.Add(r, op.Op[bigIn, bigIn]{Name: "big", Effect: op.Read,
		Handler: func(_ context.Context, _ op.Request, in bigIn) (bigIn, error) { return in, nil }})
	return r.Lookup("big")
}

// TestDecodeKeepsIntegersAbove2To53 checks that Decode does not round an
// integer through float64 on its way to the typed input.
func TestDecodeKeepsIntegersAbove2To53(t *testing.T) {
	in, _, _, err := bigEntry().Decode(json.RawMessage(
		`{"id":5312241539987020022,"max":18446744073709551615,"ids":[-9007199254740993],"ratio":1.5}`))
	if err != nil {
		t.Fatal(err)
	}
	got := in.(*bigIn)
	if got.ID != 5312241539987020022 || got.Max != math.MaxUint64 || len(got.IDs) != 1 || got.IDs[0] != -9007199254740993 || got.Ratio != 1.5 {
		t.Errorf("decoded %+v", *got)
	}
}

func TestDecodeStillChecksNumberTypes(t *testing.T) {
	e := bigEntry()
	for body, want := range map[string]string{
		`{"id":"5312241539987020022"}`: `type: 5312241539987020022 has type "string", want "integer"`,
		`{"id":1.5}`:                   `type: 1.5 has type "number", want "integer"`,
		`{"name":5}`:                   `type: 5 has type "integer", want "string"`,
		`{"ids":[1,"x"]}`:              `type: x has type "string", want "integer"`,
		`{"ratio":1e400}`:              `input must be a JSON object`,
		`{} {}`:                        `input must be a JSON object`,
		`{"id":1} x`:                   `input must be a JSON object`,
	} {
		_, _, _, err := e.Decode(json.RawMessage(body))
		oe := op.AsError(err, op.KindError)
		if oe.Kind != op.KindUsage || oe.Code != "invalid_input" || !strings.Contains(oe.Message, want) {
			t.Errorf("%s: %v; want a usage error containing %q", body, err, want)
		}
	}
	for _, body := range []string{`{"id":1.0}`, `{"id":1e3}`, `{"ratio":2}`} {
		if _, _, _, err := e.Decode(json.RawMessage(body)); err != nil {
			t.Errorf("%s: %v", body, err)
		}
	}
}
