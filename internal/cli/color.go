package cli

import (
	"io"
	"os"

	"golang.org/x/term"
)

// ColorEnabled reports whether human output may use ANSI color.
// Color is on only when stdout is a TTY, NO_COLOR is unset or empty, and
// --no-color was not passed.
func ColorEnabled(stdoutTTY, noColorFlag bool, noColorEnv string) bool {
	return stdoutTTY && !noColorFlag && noColorEnv == ""
}

func writerIsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func stdoutMode(w io.Writer, noColorFlag bool) (tty, color bool) {
	tty = writerIsTTY(w)
	color = ColorEnabled(tty, noColorFlag, os.Getenv("NO_COLOR"))
	return tty, color
}
