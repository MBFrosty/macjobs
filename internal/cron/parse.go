// Package cron parses crontabs without executing shell code or doing I/O.
package cron

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Entry contains private, unredacted parser data. Never serialize an Entry.
type Entry struct {
	ID, Schedule, Command, RunAs, Program string
	Line                                  int
	Env                                   map[string]string
	Malformed, Percent                    bool
}
type Result struct {
	Entries []Entry
	Values  []string
}

var assignment = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)
var inlineAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
var user = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*\$?$`)
var macros = map[string]bool{"@reboot": true, "@yearly": true, "@annually": true, "@monthly": true, "@weekly": true, "@daily": true, "@midnight": true, "@hourly": true}

// Parse keeps physical line numbers and hashes the normalized schedule and raw
// command. Comments/reordering do not change IDs; identical entries get suffixes.
func Parse(data []byte, source string) Result {
	r := Result{Entries: []Entry{}}
	env := map[string]string{}
	duplicates := map[string]int{}
	for n, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimLeft(strings.TrimSuffix(raw, "\r"), " \t")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := assignment.FindStringSubmatch(line); m != nil {
			value := unquote(strings.TrimSpace(m[2]))
			env[m[1]] = value
			r.Values = append(r.Values, value)
			continue
		}
		e := Entry{Line: n + 1, Env: map[string]string{}}
		for k, v := range env {
			e.Env[k] = v
		}
		fields := []string{}
		rest := line
		first, next := field(rest)
		if strings.HasPrefix(first, "@") {
			e.Schedule, rest = first, next
			e.Malformed = !macros[first]
		} else {
			for i := 0; i < 5; i++ {
				word, next := field(rest)
				fields = append(fields, word)
				rest = next
				if !validField(word, i) {
					e.Malformed = true
				}
			}
			e.Schedule = strings.Join(fields, " ")
		}
		if source == "system" {
			e.RunAs, rest = field(rest)
			if !user.MatchString(e.RunAs) {
				e.Malformed = true
			}
		}
		e.Command = rest
		if strings.TrimSpace(rest) == "" {
			e.Malformed = true
		}
		if e.Malformed {
			// Never retain malformed content in output-facing fields.
			e.ID = fmt.Sprintf("cron:%s:invalid-L%d", source, e.Line)
			e.Command, e.Schedule, e.RunAs = "", "", ""
		} else {
			hash := sha256.Sum256([]byte(e.Schedule + "\x00" + e.Command))
			base := fmt.Sprintf("cron:%s:%x", source, hash[:4])
			duplicates[base]++
			e.ID = base
			if duplicates[base] > 1 {
				e.ID += fmt.Sprintf("-%d", duplicates[base])
			}
			e.Percent = strings.Contains(e.Command, "%")
			words := e.Command
			for {
				word, next, ok := shellWord(words)
				if !ok {
					break
				}
				if !inlineAssignment.MatchString(word) {
					// Dynamic shell words cannot be checked as literal executable paths.
					if !strings.ContainsAny(word, "$`") {
						e.Program = word
					}
					break
				}
				parts := strings.SplitN(word, "=", 2)
				e.Env[parts[0]] = parts[1]
				r.Values = append(r.Values, parts[1])
				words = next
			}
		}
		r.Entries = append(r.Entries, e)
	}
	return r
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '\'' && s[len(s)-1] == '\'' || s[0] == '"' && s[len(s)-1] == '"') {
		return s[1 : len(s)-1]
	}
	return s
}
func field(s string) (string, string) {
	s = strings.TrimLeft(s, " \t")
	n := strings.IndexAny(s, " \t")
	if n < 0 {
		return s, ""
	}
	return s[:n], strings.TrimLeft(s[n:], " \t")
}

// shellWord only unquotes literal shell words. It never expands or executes them.
func shellWord(s string) (string, string, bool) {
	s = strings.TrimLeft(s, " \t")
	var b strings.Builder
	var quote byte
	for n := 0; n < len(s); n++ {
		c := s[n]
		if c == '\\' && quote != '\'' {
			if n+1 >= len(s) {
				return "", "", false
			}
			n++
			b.WriteByte(s[n])
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				b.WriteByte(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if strings.ContainsRune(" \t;&|<>()", rune(c)) {
			return b.String(), s[n:], b.Len() > 0
		}
		b.WriteByte(c)
	}
	return b.String(), "", quote == 0 && b.Len() > 0
}

var months = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
var days = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func validField(s string, index int) bool {
	mins := []int{0, 0, 1, 1, 0}
	maxs := []int{59, 23, 31, 12, 7}
	value := func(s string) (int, bool) {
		names := []string{}
		offset := 0
		if index == 3 {
			names = months
			offset = 1
		}
		if index == 4 {
			names = days
		}
		for n, name := range names {
			if strings.EqualFold(s, name) {
				return n + offset, true
			}
		}
		if s == "" {
			return 0, false
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return 0, false
			}
		}
		n, err := strconv.Atoi(s)
		return n, err == nil && n >= mins[index] && n <= maxs[index]
	}
	if s == "" {
		return false
	}
	for _, part := range strings.Split(s, ",") {
		step := strings.Split(part, "/")
		if len(step) > 2 {
			return false
		}
		if len(step) == 2 {
			if step[1] == "" {
				return false
			}
			for _, c := range step[1] {
				if c < '0' || c > '9' {
					return false
				}
			}
			n, err := strconv.Atoi(step[1])
			if err != nil || n <= 0 {
				return false
			}
		}
		base := step[0]
		if base == "*" {
			continue
		}
		bounds := strings.Split(base, "-")
		if len(bounds) > 2 {
			return false
		}
		lo, ok := value(bounds[0])
		if !ok {
			return false
		}
		if len(bounds) == 2 {
			hi, ok := value(bounds[1])
			if !ok || hi < lo {
				return false
			}
		}
	}
	return true
}
