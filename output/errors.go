package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	ExitOK           = 0
	ExitError        = 1
	ExitUsage        = 2
	ExitNotFound     = 3
	ExitConflict     = 4
	ExitAuth         = 5
	ExitRate         = 6
	ExitTimeout      = 7
	ExitStaleIndex   = 8
	ExitModelUnavail = 9
	ExitPartial      = 10
)

type CLIError struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Suggestions []string `json:"suggestions,omitempty"`
	ExitCode    int      `json:"exit_code"`
	Retryable   bool     `json:"retryable,omitempty"`
}

func (e *CLIError) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	for _, s := range e.Suggestions {
		b.WriteString("\n  hint: ")
		b.WriteString(s)
	}
	return b.String()
}

func Err(code, message string) *CLIError {
	return &CLIError{Code: code, Message: message, ExitCode: ExitError}
}

func ErrWithExit(code, message string, exitCode int) *CLIError {
	return &CLIError{Code: code, Message: message, ExitCode: exitCode}
}

func WriteError(w io.Writer, format Format, e *CLIError) {
	if format == FormatJSON {
		envelope := struct {
			Error *CLIError `json:"error"`
		}{Error: e}
		_ = json.NewEncoder(w).Encode(envelope)
		return
	}
	_, _ = fmt.Fprintln(w, "error:", e.Error())
}
