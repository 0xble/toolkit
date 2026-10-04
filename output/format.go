package output

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/mattn/go-isatty"
)

type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
	FormatRaw   Format = "raw"
	FormatPlain Format = "plain"
)

// agentMode is set once from the root --agent flag. It bundles the machine
// surface an automated caller wants — JSON output, no colour, no prompts —
// behind one switch, so agents have an explicit alternative to the pipe
// inference that used to do this implicitly.
var agentMode bool

// SetAgentMode is called from main before any command runs.
func SetAgentMode(v bool) { agentMode = v }

// AgentMode reports whether the caller asked for the agent surface.
func AgentMode() bool { return agentMode }

func ResolveFormat(flag string, jsonFlag bool) Format {
	if flag != "" {
		return Format(strings.ToLower(flag))
	}
	if agentMode {
		return FormatJSON
	}
	if jsonFlag {
		return FormatJSON
	}
	// Machine output is explicit. Earlier versions returned JSON whenever
	// stdout was not a terminal, which meant the schema changed based on
	// whether the caller happened to pipe. cli-conventions prohibits that:
	// a pipe is a cosmetic signal, not a contract signal. Callers that want
	// JSON pass --json (or --agent, which bundles it).
	return FormatTable
}

func IsTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

func NoColor() bool {
	_, ok := os.LookupEnv("NO_COLOR")
	if !ok {
		return false
	}
	if shouldBypassNoColorInCodexSession() {
		return false
	}
	return true
}

func shouldBypassNoColorInCodexSession() bool {
	if strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")) == "" {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb")
}

func EncodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func PrintTable(w io.Writer, header []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if len(header) > 0 {
		fmt_row(tw, header)
	}
	for _, row := range rows {
		fmt_row(tw, row)
	}
	_ = tw.Flush()
}

func fmt_row(w io.Writer, cols []string) {
	for i, col := range cols {
		if i > 0 {
			_, _ = io.WriteString(w, "\t")
		}
		_, _ = io.WriteString(w, col)
	}
	_, _ = io.WriteString(w, "\n")
}
