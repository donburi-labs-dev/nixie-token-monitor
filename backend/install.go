package main

import (
	"bufio"
	"errors"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var hookEvents = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"Notification", "SubagentStop", "Stop", "SessionEnd",
}
var toolEvents = map[string]bool{"PreToolUse": true, "PostToolUse": true}

// entries we own (current exe, and the earlier Python prototype)
func isOurs(cmd string) bool {
	c := strings.ToLower(cmd)
	return strings.Contains(c, "ccnixie-hook") || strings.Contains(c, "cc_icue_hook.py")
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "ccnixie-hook.exe"
	}
	return "ccnixie-hook"
}

func settingsPath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude", "settings.json")
}

func loadSettings() (*OMap, []byte, error) {
	p := settingsPath()
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return NewOMap(), nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(string(b)) == "" {
		return NewOMap(), b, nil
	}
	v, err := ParseOJSON(b)
	if err != nil {
		where := ""
		var se *json.SyntaxError
		if errors.As(err, &se) {
			line := 1 + strings.Count(string(b[:min(int(se.Offset), len(b))]), "\n")
			where = fmt.Sprintf(" near line %d", line)
		}
		return nil, b, fmt.Errorf("%s is not valid JSON%s (%v). Fix it first; nothing was changed", p, where, err)
	}
	m, ok := v.(*OMap)
	if !ok {
		return nil, b, fmt.Errorf("%s: top level is not an object; nothing was changed", p)
	}
	return m, b, nil
}

// remove our hook entries; returns how many were removed
func stripOurs(root *OMap, keepEmpty bool) int {
	hv, ok := root.Get("hooks")
	hooks, ok2 := hv.(*OMap)
	if !ok || !ok2 {
		return 0
	}
	n := 0
	for _, ev := range append([]string{}, hooks.Keys...) {
		arr, ok := hooks.Vals[ev].([]any)
		if !ok {
			continue
		}
		var keep []any
		for _, g := range arr {
			gm, ok := g.(*OMap)
			if !ok {
				keep = append(keep, g)
				continue
			}
			inner, ok := gm.Vals["hooks"].([]any)
			if !ok {
				keep = append(keep, g)
				continue
			}
			var ik []any
			for _, h := range inner {
				if hm, ok := h.(*OMap); ok {
					if c, _ := hm.Vals["command"].(string); isOurs(c) {
						n++
						continue
					}
				}
				ik = append(ik, h)
			}
			if len(ik) == 0 {
				continue // group only held our hook
			}
			gm.Set("hooks", ik)
			keep = append(keep, gm)
		}
		if len(keep) == 0 && !keepEmpty {
			hooks.Delete(ev)
		} else {
			if keep == nil {
				keep = []any{}
			}
			hooks.Set(ev, keep) // empty slot keeps the key's position for install
		}
	}
	if len(hooks.Keys) == 0 && !keepEmpty {
		root.Delete("hooks")
	}
	return n
}

func writeSettings(root *OMap, orig []byte) (string, error) {
	p := settingsPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	bak := ""
	if orig != nil {
		bak = p + ".ccnixie-bak-" + time.Now().Format("20060102-150405")
		for i := 2; ; i++ { // never overwrite an earlier backup
			if _, err := os.Stat(bak); os.IsNotExist(err) {
				break
			}
			bak = fmt.Sprintf("%s.ccnixie-bak-%s-%d", p, time.Now().Format("20060102-150405"), i)
		}
		if err := os.WriteFile(bak, orig, 0o644); err != nil {
			return "", fmt.Errorf("could not write backup: %v", err)
		}
	}
	out := MarshalOJSON(root)
	if _, err := ParseOJSON(out); err != nil { // paranoia: never write broken JSON
		return bak, fmt.Errorf("internal error, generated invalid JSON; nothing was changed")
	}
	return bak, os.WriteFile(p, out, 0o644) // UTF-8, no BOM
}

func copySelf(dst string) error {
	src, err := os.Executable()
	if err != nil {
		return err
	}
	if s, _ := filepath.EvalSymlinks(src); s != "" {
		src = s
	}
	if d, _ := filepath.Abs(dst); strings.EqualFold(filepath.Clean(src), filepath.Clean(d)) {
		return nil // already running from the install location
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	var last error
	for i := 0; i < 10; i++ { // the hook may be running right now
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err == nil {
			_, err = io.Copy(out, in)
			out.Close()
			return err
		}
		last = err
		time.Sleep(300 * time.Millisecond)
	}
	return last
}

func cmdInstall() int {
	dir := baseDir()
	if err := os.MkdirAll(filepath.Join(dir, "work"), 0o755); err != nil {
		fmt.Println("ERROR:", err)
		return 1
	}
	dst := filepath.Join(dir, exeName())
	root, orig, err := loadSettings()
	if err != nil {
		fmt.Println("ERROR:", err)
		return 1
	}
	if err := copySelf(dst); err != nil {
		fmt.Println("ERROR: could not copy to", dst, ":", err)
		return 1
	}
	removed := stripOurs(root, true)
	command := `"` + filepath.ToSlash(dst) + `"`
	hv, _ := root.Get("hooks")
	hooks, ok := hv.(*OMap)
	if !ok {
		hooks = NewOMap()
		root.Set("hooks", hooks)
	}
	for _, ev := range hookEvents {
		arr, _ := hooks.Vals[ev].([]any)
		h := NewOMap()
		h.Set("type", "command")
		h.Set("command", command)
		h.Set("timeout", json.Number("10"))
		g := NewOMap()
		if toolEvents[ev] {
			g.Set("matcher", "*")
		}
		g.Set("hooks", []any{h})
		hooks.Set(ev, append(arr, g))
	}
	bak, err := writeSettings(root, orig)
	if err != nil {
		fmt.Println("ERROR:", err)
		return 1
	}
	fmt.Println("CC Nixie backend", Version, "installed.")
	fmt.Println("  program :", dst)
	fmt.Println("  settings:", settingsPath(), fmt.Sprintf("(%d events registered", len(hookEvents))+
		map[bool]string{true: fmt.Sprintf(", %d old entries replaced)", removed), false: ")"}[removed > 0])
	if bak != "" {
		fmt.Println("  backup  :", bak)
	}
	fmt.Println("Restart Claude Code, then use it once. The widget updates on the next event.")
	return 0
}

func cmdUninstall() int {
	root, orig, err := loadSettings()
	if err != nil {
		fmt.Println("ERROR:", err)
		return 1
	}
	n := stripOurs(root, false)
	if n == 0 {
		fmt.Println("No CC Nixie hooks found in", settingsPath(), "- nothing changed.")
		return 0
	}
	bak, err := writeSettings(root, orig)
	if err != nil {
		fmt.Println("ERROR:", err)
		return 1
	}
	fmt.Printf("Removed %d hook entries from %s\n  backup: %s\n", n, settingsPath(), bak)
	fmt.Println("You can now delete the folder:", baseDir())
	return 0
}

func cmdStatus() int {
	fmt.Println("CC Nixie backend", Version)
	dst := filepath.Join(baseDir(), exeName())
	if _, err := os.Stat(dst); err == nil {
		fmt.Println("  program  : OK", dst)
	} else {
		fmt.Println("  program  : not installed (", dst, ")")
	}
	root, _, err := loadSettings()
	if err != nil {
		fmt.Println("  settings :", err)
	} else {
		cnt := 0
		if hooks, ok := root.Vals["hooks"].(*OMap); ok {
			for _, ev := range hookEvents {
				arr, _ := hooks.Vals[ev].([]any)
				for _, g := range arr {
					if gm, ok := g.(*OMap); ok {
						inner, _ := gm.Vals["hooks"].([]any)
						for _, h := range inner {
							if hm, ok := h.(*OMap); ok {
								if c, _ := hm.Vals["command"].(string); isOurs(c) {
									cnt++
								}
							}
						}
					}
				}
			}
		}
		fmt.Printf("  settings : %d/%d events registered\n", cnt, len(hookEvents))
	}
	b, err := os.ReadFile(filepath.Join(baseDir(), "state.json"))
	if err != nil {
		fmt.Println("  state    : no state.json yet (use Claude Code once)")
		return 0
	}
	var st State
	if json.Unmarshal(b, &st) != nil {
		fmt.Println("  state    : unreadable")
		return 0
	}
	fmt.Printf("  state    : updated %s, %d events, today %d tokens, %d sessions\n",
		time.UnixMilli(st.Updated).Format("2006-01-02 15:04:05"), st.Seq, st.Today.Sum(), len(st.Sessions))
	return 0
}

// Double-clicked from Explorer: offer to install instead of waiting on stdin.
func interactive() int {
	fmt.Println("CC Nixie backend", Version, "- Claude Code hook for the CC Nixie iCUE widget")
	fmt.Println()
	fmt.Println("  install    register the hook in ~/.claude/settings.json")
	fmt.Println("  uninstall  remove it again")
	fmt.Println("  status     check that everything works")
	fmt.Println()
	fmt.Print("Install now? [y/N]: ")
	ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	code := 0
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
		fmt.Println()
		code = cmdInstall()
	}
	fmt.Print("\nPress Enter to close.")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	return code
}
