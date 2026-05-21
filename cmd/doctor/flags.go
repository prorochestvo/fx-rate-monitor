package main

import (
	"errors"
	"flag"
	"io"
)

// errHelpRequested is returned by flagSet.Parse when the user passes --help or -h.
// Callers use errors.Is(err, errHelpRequested) to distinguish help requests from
// real parse errors and exit 0 instead of 2.
var errHelpRequested = flag.ErrHelp

// flagSet is a thin alias so files in this package can refer to it without
// importing "flag" directly (keeping their import lists short).
type flagSet = flag.FlagSet

// newFlagSet returns a flag.FlagSet configured with ContinueOnError (so the
// caller can detect flag.ErrHelp and return 0 rather than letting the stdlib
// call os.Exit). errOut is where the FlagSet writes its own error/usage messages.
func newFlagSet(name string, errOut io.Writer) *flag.FlagSet {
	fset := flag.NewFlagSet(name, flag.ContinueOnError)
	fset.SetOutput(errOut)
	return fset
}

// isHelpErr reports whether err is the sentinel returned by flag when --help/-h
// is passed to a FlagSet with ContinueOnError mode.
func isHelpErr(err error) bool {
	return errors.Is(err, flag.ErrHelp)
}
