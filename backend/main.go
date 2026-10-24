// ccnixie-hook: backend for the CC Nixie iCUE widget.
//
//	ccnixie-hook            hook mode (Claude Code pipes the event JSON on stdin)
//	ccnixie-hook install    copy to ~/.claude/icue and register hooks in settings.json
//	ccnixie-hook uninstall  remove the hooks
//	ccnixie-hook status     show installation / state
//
// Everything stays on this PC: it reads Claude Code's local transcripts only to
// count tokens and writes ~/.claude/icue/state.json. No network access.
package main

import (
	"fmt"
	"os"
)

const Version = "0.1.2"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			os.Exit(cmdInstall())
		case "uninstall":
			os.Exit(cmdUninstall())
		case "status":
			os.Exit(cmdStatus())
		case "version", "--version", "-v":
			fmt.Println(Version)
			return
		case "help", "--help", "-h":
			fmt.Println("usage: ccnixie-hook [install|uninstall|status|version]")
			return
		}
	}
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		os.Exit(interactive()) // started by hand, not by Claude Code
	}
	runHook()
}
