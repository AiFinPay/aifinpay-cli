package main

import (
	"flag"
	"fmt"
	"os"
)

// flagSet is a thin wrapper over the stdlib flag package that (a) tracks which
// flags were explicitly provided (so optional numeric flags are only forwarded
// when set) and (b) gives every command a consistent usage line. Both single-
// and double-dash forms work (Go's flag accepts `-x` and `--x`).
type flagSet struct {
	fs        *flag.FlagSet
	usageLine string
	set       map[string]bool
}

func newFlagSet(name string) *flagSet {
	return &flagSet{
		fs:  flag.NewFlagSet(name, flag.ContinueOnError),
		set: map[string]bool{},
	}
}

func (f *flagSet) StringVar(p *string, name, def, usage string)  { f.fs.StringVar(p, name, def, usage) }
func (f *flagSet) BoolVar(p *bool, name string, def bool, u string) { f.fs.BoolVar(p, name, def, u) }
func (f *flagSet) Float64Var(p *float64, name string, def float64, u string) {
	f.fs.Float64Var(p, name, def, u)
}

func (f *flagSet) wasSet(name string) bool { return f.set[name] }

func (f *flagSet) parse(args []string) {
	f.fs.SetOutput(os.Stderr)
	f.fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage:", f.usageLine)
		fmt.Fprintln(os.Stderr, "\nFlags:")
		f.fs.PrintDefaults()
	}
	if err := f.fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(exitOK)
		}
		os.Exit(exitInput)
	}
	f.fs.Visit(func(fl *flag.Flag) { f.set[fl.Name] = true })
}
