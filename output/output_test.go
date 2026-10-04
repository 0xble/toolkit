package output

import (
	"bytes"
	"testing"
)

func TestResolveFormat_JSONFlag(t *testing.T) {
	f := ResolveFormat("", true)
	if f != FormatJSON {
		t.Errorf("expected json, got %s", f)
	}
}

func TestResolveFormat_ExplicitFlag(t *testing.T) {
	f := ResolveFormat("yaml", false)
	if f != FormatYAML {
		t.Errorf("expected yaml, got %s", f)
	}
}

func TestEncodeJSON(t *testing.T) {
	var buf bytes.Buffer
	err := EncodeJSON(&buf, map[string]string{"key": "val"})
	if err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty output")
	}
}

func TestFilterFields_Object(t *testing.T) {
	in := map[string]any{"a": 1, "b": 2, "c": 3}
	out, err := FilterFields(in, []string{"a", "c"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatal("expected map")
	}
	if len(m) != 2 {
		t.Errorf("expected 2 keys, got %d", len(m))
	}
	if m["a"] != float64(1) {
		t.Errorf("expected a=1, got %v", m["a"])
	}
}

func TestFilterFields_Array(t *testing.T) {
	in := []map[string]any{
		{"x": 10, "y": 20},
		{"x": 30, "y": 40},
	}
	out, err := FilterFields(in, []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := out.([]map[string]any)
	if !ok {
		t.Fatal("expected slice")
	}
	if len(arr) != 2 {
		t.Errorf("expected 2 items, got %d", len(arr))
	}
	if arr[0]["x"] != float64(10) {
		t.Errorf("expected x=10, got %v", arr[0]["x"])
	}
}

func TestFilterFields_Empty(t *testing.T) {
	in := map[string]any{"a": 1}
	out, err := FilterFields(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Error("expected non-nil")
	}
}

func TestCLIError_Error(t *testing.T) {
	e := &CLIError{
		Code:        "auth_failed",
		Message:     "API key invalid",
		Suggestions: []string{"check your .env file"},
		ExitCode:    ExitAuth,
	}
	s := e.Error()
	if s == "" {
		t.Error("expected non-empty error string")
	}
}

func TestWriteError_JSON(t *testing.T) {
	var buf bytes.Buffer
	e := Err("test", "test error")
	WriteError(&buf, FormatJSON, e)
	if buf.Len() == 0 {
		t.Error("expected JSON error output")
	}
}

func TestWriteError_Plain(t *testing.T) {
	var buf bytes.Buffer
	e := Err("test", "test error")
	WriteError(&buf, FormatTable, e)
	if buf.Len() == 0 {
		t.Error("expected plain error output")
	}
}

func TestPrintTable(t *testing.T) {
	var buf bytes.Buffer
	PrintTable(&buf, []string{"NAME", "AGE"}, [][]string{
		{"alice", "30"},
		{"bob", "25"},
	})
	if buf.Len() == 0 {
		t.Error("expected table output")
	}
}

func TestNoColor(t *testing.T) {
	t.Run("honors no_color outside codex", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		t.Setenv("CODEX_THREAD_ID", "")
		if !NoColor() {
			t.Fatal("NoColor() = false, want true outside Codex")
		}
	})

	t.Run("bypasses no_color inside codex for ai tool output", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		t.Setenv("CODEX_THREAD_ID", "thread-123")
		t.Setenv("TERM", "xterm-256color")
		if NoColor() {
			t.Fatal("NoColor() = true, want false inside Codex")
		}
	})

	t.Run("keeps no_color for dumb terminals", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		t.Setenv("CODEX_THREAD_ID", "thread-123")
		t.Setenv("TERM", "dumb")
		if !NoColor() {
			t.Fatal("NoColor() = false, want true for dumb terminals")
		}
	})
}
