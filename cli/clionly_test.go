package cli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/0xble/toolkit/op"
)

type pathsIn struct {
	File    string `json:"file,omitempty" arg:"" optional:"" toolkit:"cli-only"`
	IDsFrom string `json:"ids_from,omitempty" toolkit:"cli-only"`
	Out     string `json:"out,omitempty" name:"to" toolkit:"cli-only"`
}

// TestCLIOnlyInputsWorkOnTheCLI checks that the CLI accepts cli-only inputs
// unchanged, under the very spelling the remote refusal names, so the
// message always points at a real flag or argument.
func TestCLIOnlyInputsWorkOnTheCLI(t *testing.T) {
	var got pathsIn
	r := op.New("t", "v1")
	op.Add(r, op.Op[pathsIn, result]{Name: "paths", Effect: op.Read,
		Handler: func(_ context.Context, _ op.Request, in pathsIn) (result, error) {
			got = in
			return result{}, nil
		},
	})
	e := r.Lookup("paths")
	args := []string{"paths"}
	for _, name := range e.CLIOnlyInputs {
		_, err := e.CallJSON(context.Background(), op.Request{Surface: op.SurfaceHTTP}, json.RawMessage(`{"`+name+`":"/tmp/x"}`), nil)
		spelling, _, _ := strings.Cut(op.AsError(err, op.KindError).Message, " ")
		if strings.HasPrefix(spelling, "<") {
			args = append(args, "/tmp/"+name)
		} else {
			args = append(args, spelling+"=/tmp/"+name)
		}
	}
	if code, _, stderr := run(t, r, args...); code != 0 {
		t.Fatalf("%v: exit %d %s", args, code, stderr)
	}
	if want := (pathsIn{File: "/tmp/file", IDsFrom: "/tmp/ids_from", Out: "/tmp/out"}); got != want {
		t.Errorf("the CLI passed %+v, want %+v", got, want)
	}
}
