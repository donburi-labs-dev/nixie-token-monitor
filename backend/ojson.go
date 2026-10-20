package main

// Minimal order-preserving JSON (objects keep key order) so that editing
// ~/.claude/settings.json does not reshuffle the user's file.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type OMap struct {
	Keys []string
	Vals map[string]any
}

func NewOMap() *OMap { return &OMap{Vals: map[string]any{}} }

func (m *OMap) Get(k string) (any, bool) { v, ok := m.Vals[k]; return v, ok }
func (m *OMap) Set(k string, v any) {
	if _, ok := m.Vals[k]; !ok {
		m.Keys = append(m.Keys, k)
	}
	m.Vals[k] = v
}
func (m *OMap) Delete(k string) {
	if _, ok := m.Vals[k]; !ok {
		return
	}
	delete(m.Vals, k)
	for i, kk := range m.Keys {
		if kk == k {
			m.Keys = append(m.Keys[:i], m.Keys[i+1:]...)
			break
		}
	}
}

func ParseOJSON(data []byte) (any, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // tolerate a BOM
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after JSON value")
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			m := NewOMap()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string")
				}
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				m.Set(k, v)
			}
			if _, err := dec.Token(); err != nil { // '}'
				return nil, err
			}
			return m, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil { // ']'
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	default:
		return tok, nil // string, json.Number, bool, nil
	}
}

func encString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(b.String(), "\n")
}

func MarshalOJSON(v any) []byte {
	var b strings.Builder
	writeValue(&b, v, 0)
	b.WriteString("\n")
	return []byte(b.String())
}

func writeValue(b *strings.Builder, v any, ind int) {
	pad := strings.Repeat("  ", ind)
	switch t := v.(type) {
	case *OMap:
		if len(t.Keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, k := range t.Keys {
			b.WriteString(pad + "  " + encString(k) + ": ")
			writeValue(b, t.Vals[k], ind+1)
			if i < len(t.Keys)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(pad + "}")
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, e := range t {
			b.WriteString(pad + "  ")
			writeValue(b, e, ind+1)
			if i < len(t)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(pad + "]")
	case string:
		b.WriteString(encString(t))
	case json.Number:
		b.WriteString(t.String())
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case nil:
		b.WriteString("null")
	case int:
		fmt.Fprintf(b, "%d", t)
	default:
		j, _ := json.Marshal(t)
		b.Write(j)
	}
}
