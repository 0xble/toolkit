package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0xble/toolkit/api"
	"github.com/0xble/toolkit/op"
)

type bigIn struct {
	ID    int64   `json:"id"`
	Max   uint64  `json:"max"`
	Ratio float64 `json:"ratio,omitempty"`
}

// TestIntegersAbove2To53 checks that an int64 or uint64 above 2^53 reaches
// the handler and the response exactly.
func TestIntegersAbove2To53(t *testing.T) {
	r := op.New("t", "v")
	op.Add(r, op.Op[bigIn, bigIn]{Name: "big", Effect: op.Read,
		Handler: func(_ context.Context, _ op.Request, in bigIn) (bigIn, error) { return in, nil }})
	h := api.Handler(r, api.Options{})
	for body, want := range map[string]string{
		`{"id":5312241539987020022,"max":18446744073709551615,"ratio":1.5}`: `{"id":5312241539987020022,"max":18446744073709551615,"ratio":1.5}`,
		`{"id":-9007199254740993,"max":9007199254740993}`:                   `{"id":-9007199254740993,"max":9007199254740993}`,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ops/big", strings.NewReader(body)))
		if w.Code != 200 || strings.TrimSpace(w.Body.String()) != want {
			t.Errorf("%s: %d %s; want %s", body, w.Code, w.Body.String(), want)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ops/big", strings.NewReader(`{"id":"5312241539987020022"}`)))
	if w.Code != 400 || !strings.Contains(w.Body.String(), `has type \"string\", want \"integer\"`) {
		t.Errorf("a string for an integer: %d %s", w.Code, w.Body.String())
	}
}
