package output

import (
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

const (
	Bold   = "1"
	Red    = "31"
	Green  = "32"
	Yellow = "33"
)

// ColorEnabled reports whether human-readable output is attached to an
// interactive terminal that has not opted out of ANSI styling.
func ColorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	fd := file.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// Style wraps one trusted display fragment in an ANSI SGR sequence.
func Style(enabled bool, code, value string) string {
	if !enabled {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}
