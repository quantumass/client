package libkb

import (
	"fmt"
	"os"
	"os/exec"
)

//
// some borrowed from here:
//
//  https://github.com/bradfitz/camlistore/blob/master/pkg/misc/pinentry/pinentry.go
//
// Under the Apache 2.0 license
//

func canExec(s string) error {
	fi, err := os.Stat(s)
	if err != nil {
		return err
	}
	mode := fi.Mode()

	//
	// Only consider non-directories that have at least one +x
	//  bit set.
	//
	// TODO: Recheck this on windows!
	//   See here for lookpath: http://golang.org/src/pkg/os/exec/lp_windows.go
	//
	// Similar to check from exec.LookPath below
	//   See here: http://golang.org/src/pkg/os/exec/lp_unix.go
	//
	if mode.IsDir() {
		return fmt.Errorf("Program '%s' is a directory", s)
	} else if int(mode)&0111 == 0 {
		return fmt.Errorf("Program '%s' isn't executable", s)
	} else {
		return nil
	}
}

func FindPinentry() (string, error) {
	bins := []string{
		// If you install MacTools you'll wind up with this pinentry
		"/usr/local/MacGPG2/libexec/pinentry-mac.app/Contents/MacOS/pinentry-mac",
	}

	extra_paths := []string{}

	G.Log.Debug("+ FindPinentry()")

	cmds := []string{
		"pinentry-gtk-2",
		"pinentry-qt4",
		"pinentry",
	}

	checkFull := func(s string) bool {
		G.Log.Debug("| Check fullpath %s", s)
		found := (canExec(s) == nil)
		if found {
			G.Log.Debug("- Found: %s", s)
		}
		return found
	}

	for _, b := range bins {
		if checkFull(b) {
			return b, nil
		}
	}

	path := os.Getenv("PATH")
	for _, c := range cmds {
		G.Log.Debug("| Looking for %s in standard PATH %s", c, path)
		fullc, err := exec.LookPath(c)
		if err == nil {
			G.Log.Debug("- Found %s", fullc)
			return fullc, nil
		}
	}

	for _, ep := range extra_paths {
		for _, c := range cmds {
			full := ep + "/" + c
			if checkFull(full) {
				return full, nil
			}
		}
	}

	G.Log.Debug("- FindPinentry: none found")
	return "", fmt.Errorf("No pinentry found, checked a bunch of different places")
}

type Pinentry struct {
	path string
}

func NewPinentry() *Pinentry {
	return &Pinentry{path: ""}
}

func (pe *Pinentry) Init() error {
	prog := G.Env.GetPinentry()
	var err error
	if len(prog) > 0 {
		if err := canExec(prog); err == nil {
			pe.path = prog
		} else {
			err = fmt.Errorf("Can't execute given pinentry program '%s': %s",
				prog, err.Error())
		}
	} else if prog, err = FindPinentry(); err == nil {
		pe.path = prog
	}
	return err
}

type FallbackPasswordEntry struct {
	pinentry *Pinentry
	terminal Terminal
	initRes  *error
}

func NewFallbackPasswordEntry() *FallbackPasswordEntry {
	return &FallbackPasswordEntry{}
}

func (pe *FallbackPasswordEntry) Init() error {
	if pe.initRes != nil {
		return *pe.initRes
	}
	pe.pinentry = NewPinentry()
	pe.terminal = G.Terminal
	err := pe.pinentry.Init()
	pe.initRes = &err
	return err
}
