package output

import (
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

func ConfirmOrError(prompt string, dangerouslySkipConfirmation bool) error {
	if dangerouslySkipConfirmation {
		return nil
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("confirmation required but stdin is not a terminal; use --dangerously-skip-confirmation to skip")
	}
	fmt.Fprint(os.Stderr, prompt+" [y/N] ")
	var resp string
	if _, err := fmt.Scanln(&resp); err != nil {
		resp = ""
	}
	resp = strings.ToLower(strings.TrimSpace(resp))
	if resp == "y" || resp == "yes" {
		return nil
	}
	return fmt.Errorf("aborted")
}
