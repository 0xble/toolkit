package cli

import (
	"bytes"
	"testing"
)

func TestPromptConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  bool
	}{
		{name: "accepts y", input: "y\n", want: true},
		{name: "accepts yes", input: "yes\n", want: true},
		{name: "rejects no", input: "n\n", want: false},
		{name: "rejects EOF", input: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if got := prompt(bytes.NewBufferString(tc.input), &out, "Apply item? This is destructive."); got != tc.want {
				t.Errorf("prompt(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
