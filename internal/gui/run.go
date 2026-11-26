package gui

import (
	"context"
	"errors"
	"log/slog"
)

// ErrHeadlessBuild is returned by binaries built with -tags headless. Those
// builds intentionally omit Wails and are used for Linux servers and Docker.
var ErrHeadlessBuild = errors.New("this build has no desktop UI; use `homing serve`")

// Options describes the already-running local HTTP service the desktop shell
// should display. The shell never owns session data; it is another client of
// the same /api and /console endpoints as the browser.
type Options struct {
	URL         string
	DataDir     string
	Version     string
	StartHidden bool
	Log         *slog.Logger
}

// Available reports whether this build contains the desktop shell.
func Available() bool { return desktopAvailable }

// Run starts the tray icon, panel and main window. It blocks until ctx is
// cancelled or the user chooses Quit.
func Run(ctx context.Context, options Options) error {
	return run(ctx, options)
}
