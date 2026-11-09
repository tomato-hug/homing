// Package cli lets packages add `homing <name>` subcommands from init(), so
// adding a command never means editing cmd/homing/main.go:
//
//	func init() {
//		cli.Register(cli.Command{Name: "pair", Summary: "在终端里打印配对二维码和 6 位码", Run: runPair})
//	}
//
// A command with its own subcommands (homing agents test ...) parses args itself.
package cli

import (
	"context"
	"sort"
	"sync"
)

// Command is one `homing <Name>` subcommand. Run gets the arguments after the
// name and a context that is cancelled on Ctrl-C / SIGTERM.
type Command struct {
	Name    string
	Summary string // one line, shown in `homing help`
	Run     func(ctx context.Context, args []string) error
}

var (
	mu   sync.RWMutex
	cmds = map[string]Command{}
)

// Register adds a command. A duplicate name panics: it is a programming error.
func Register(c Command) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := cmds[c.Name]; dup {
		panic("cli: duplicate command " + c.Name)
	}
	cmds[c.Name] = c
}

// Lookup finds a registered command.
func Lookup(name string) (Command, bool) {
	mu.RLock()
	defer mu.RUnlock()
	c, ok := cmds[name]
	return c, ok
}

// All returns the registered commands sorted by name.
func All() []Command {
	mu.RLock()
	out := make([]Command, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c)
	}
	mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
