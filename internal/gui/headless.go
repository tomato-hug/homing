//go:build headless

package gui

import "context"

const desktopAvailable = false

func run(context.Context, Options) error { return ErrHeadlessBuild }
