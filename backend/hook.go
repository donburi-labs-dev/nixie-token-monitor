package main

// Hook mode: called by Claude Code for every registered event with the event
// JSON on stdin. Updates ~/.claude/icue/state.json for the widget.
// Never writes to stdout/stderr and always exits 0.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	keepSessions = 6 * time.Hour
	keepIDs      = 300
	maxRead      = 64 << 20
)

type Tok struct {
	In  int64 `json:"in"`
	Out int64 `json:"out"`
	CW  int64 `json:"cw"`
	CR  int64 `json:"cr"`
}

func (t Tok) Sum() int64 { return t.In + t.Out + t.CW + t.CR }

type Session struct {
	ID      string `json:"id"`
	Project string `json:"project,omitempty"`
	Status  string `json:"status"`
	Tool    string `json:"tool"`
	Tok     int64  `json:"tok"`
	Start   int64  `json:"start"`
	Last    int64  `json:"last"`
	LastE   string `json:"le,omitempty"` // last hook event of this session
}

type LastEv struct {
	E    string `json:"e"`
	SID  string `json:"sid"`
	Tool string `json:"tool"`
	T    int64  `json:"t"`
}

type State struct {
	V        int       `json:"v"`
	Backend  string    `json:"backend"`
	Seq      int64     `json:"seq"`
	Updated  int64     `json:"updated"`
	Date     string    `json:"date"`
	Today    Tok       `json:"today"`
	Last     LastEv    `json:"last"`
	Sessions []Session `json:"sessions"`
}

type Cursor struct {
	Off int64    `json:"off"`
	IDs []string `json:"ids"`
	T   int64    `json:"t"`
}

type HookEvent struct {
	Name       string `json:"hook_event_name"`
	SessionID  string `json:"session_id"`
	Transcript string `json:"transcript_path"`
	Cwd        string `json:"cwd"`
	ToolName   string `json:"tool_name"`
	Message    string `json:"message"`
	NotifType  string `json:"notification_type"`
}

// Notification fires both for permission prompts and for the plain
// "waiting for your input" idle reminder. Only the former is an approval wait.
func notificationStatus(ev HookEvent) string {
	switch strings.ToLower(ev.NotifType) {
	case "permission_prompt", "elicitation_dialog":
		return "wait"
	case "idle_prompt", "auth_success":
		return "idle"
	}
	m := strings.ToLower(ev.Message)
	switch {
	case strings.Contains(m, "permission"), strings.Contains(m, "approv"), strings.Contains(m, "allow"):
		return "wait"
	default:
		return "idle" // unknown / idle reminder: never leave the alert on
	}
}

var statusOf = map[string]string{
	"SessionStart":      "idle",
	"UserPromptSubmit":  "think",
	"PreToolUse":        "tool",
	"PostToolUse":       "think",
	"SubagentStop":      "think",
	"PermissionRequest": "wait", // fires the moment the permission dialog appears
	"Notification":      "wait",
	"Stop":              "idle",
	"PreCompact":        "think",
}

func baseDir() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude", "icue")
}

func runHook() {
	defer func() { _ = recover() }() // never fail Claude Code
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return
	}
	var ev HookEvent
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	sid := ev.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	if sid == "" {
		return
	}
	base := baseDir()
	work := filepath.Join(base, "work")
	if os.MkdirAll(work, 0o755) != nil {
		return
	}
	lock := filepath.Join(work, "lock")
	if !acquireLock(lock, 3*time.Second) {
		return
	}
	defer os.Remove(lock)

	now := time.Now()
	t := now.UnixMilli()
	today := now.Format("2006-01-02")
	statePath := filepath.Join(base, "state.json")
	curPath := filepath.Join(work, "cursors.json")

	var st State
	if b, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if st.Date != today {
		st.Date, st.Today = today, Tok{}
	}
	sessions := map[string]*Session{}
	for i := range st.Sessions {
		s := st.Sessions[i]
		if s.ID != "" && s.ID != "unknown" {
			sessions[s.ID] = &s
		}
	}
	cursors := map[string]*Cursor{}
	if b, err := os.ReadFile(curPath); err == nil {
		_ = json.Unmarshal(b, &cursors)
	}
	cur := cursors[sid]
	if cur == nil {
		cur = &Cursor{}
		cursors[sid] = cur
	}
	d := readUsage(ev.Transcript, cur, today)
	cur.T = t
	st.Today.In += d.In
	st.Today.Out += d.Out
	st.Today.CW += d.CW
	st.Today.CR += d.CR

	if ev.Name == "SessionEnd" {
		delete(sessions, sid)
		delete(cursors, sid)
	} else {
		s := sessions[sid]
		if s == nil {
			s = &Session{ID: sid, Start: t, Status: "idle"}
			sessions[sid] = s
		}
		if ev.Cwd != "" {
			p := strings.TrimRight(strings.ReplaceAll(ev.Cwd, "\\", "/"), "/")
			if i := strings.LastIndex(p, "/"); i >= 0 {
				p = p[i+1:]
			}
			if p != "" {
				s.Project = p
			}
		}
		s.Tok += d.Sum()
		s.Last = t
		if ev.Name == "Notification" {
			ns := notificationStatus(ev)
			// The permission Notification arrives ~8 s after the dialog. If the tool already
			// finished (the user approved meanwhile), it must not turn the alert back on.
			if ns == "wait" && s.LastE != "PreToolUse" && s.LastE != "PermissionRequest" && s.LastE != "Notification" {
				ns = s.Status
			}
			s.Status = ns
		} else if v, ok := statusOf[ev.Name]; ok {
			s.Status = v
		}
		s.LastE = ev.Name
		switch ev.Name {
		case "PreToolUse", "PermissionRequest":
			s.Tool = ev.ToolName
		case "Stop", "UserPromptSubmit", "SessionStart":
			s.Tool = ""
		}
	}

	limit := t - keepSessions.Milliseconds()
	st.Sessions = st.Sessions[:0]
	for _, s := range sessions {
		if s.Last >= limit {
			st.Sessions = append(st.Sessions, *s)
		}
	}
	sort.Slice(st.Sessions, func(i, j int) bool { return st.Sessions[i].Start < st.Sessions[j].Start })
	for k, c := range cursors {
		if c.T < limit {
			delete(cursors, k)
		}
	}

	st.V = 2
	st.Backend = Version
	st.Seq++
	st.Updated = t
	st.Last = LastEv{E: ev.Name, SID: sid, Tool: ev.ToolName, T: t}

	if b, err := json.Marshal(cursors); err == nil {
		_ = os.WriteFile(curPath, b, 0o644)
	}
	if b, err := json.Marshal(st); err == nil {
		// in-place write (no rename) keeps the widget's file watch attached
		_ = os.WriteFile(statePath, b, 0o644)
	}
}

func acquireLock(path string, timeout time.Duration) bool {
	end := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return true
		}
		if fi, e := os.Stat(path); e == nil && time.Since(fi.ModTime()) > 10*time.Second {
			os.Remove(path) // stale lock
			continue
		}
		if time.Now().After(end) {
			return false
		}
		time.Sleep(30 * time.Millisecond)
	}
}

type tLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	UUID      string `json:"uuid"`
	Message   struct {
		ID    string `json:"id"`
		Usage struct {
			In  int64 `json:"input_tokens"`
			Out int64 `json:"output_tokens"`
			CW  int64 `json:"cache_creation_input_tokens"`
			CR  int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func readUsage(path string, cur *Cursor, today string) Tok {
	var d Tok
	if path == "" {
		return d
	}
	f, err := os.Open(path)
	if err != nil {
		return d
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return d
	}
	size := fi.Size()
	if cur.Off > size { // file rewritten
		cur.Off, cur.IDs = 0, nil
	}
	if size-cur.Off > maxRead {
		cur.Off = size - maxRead
	}
	if _, err := f.Seek(cur.Off, io.SeekStart); err != nil {
		return d
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return d
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return d
	}
	cur.Off += int64(end + 1)
	seen := map[string]bool{}
	for _, id := range cur.IDs {
		seen[id] = true
	}
	sc := bufio.NewScanner(bytes.NewReader(data[:end]))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		var l tLine
		if json.Unmarshal(line, &l) != nil || l.Type != "assistant" {
			continue
		}
		id := l.Message.ID
		if id == "" {
			id = l.RequestID
		}
		if id == "" {
			id = l.UUID
		}
		if id != "" {
			if seen[id] {
				continue
			}
			seen[id] = true
			cur.IDs = append(cur.IDs, id)
		}
		ts, err := time.Parse(time.RFC3339Nano, l.Timestamp)
		if err != nil || ts.Local().Format("2006-01-02") != today {
			continue
		}
		u := l.Message.Usage
		d.In += u.In
		d.Out += u.Out
		d.CW += u.CW
		d.CR += u.CR
	}
	if len(cur.IDs) > keepIDs {
		cur.IDs = cur.IDs[len(cur.IDs)-keepIDs:]
	}
	return d
}
