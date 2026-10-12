package redact

import (
	"regexp"
	"strings"
)

// All cron assignments are private, even with short values or ordinary keys.
// Credential options use the same shell-word boundary, including concatenated
// quotes, escaped spaces, and command substitutions. Nothing is evaluated.
var cronPrivate = regexp.MustCompile(`(?i)(^|[\s;])([A-Za-z_][A-Za-z0-9_]*=|-u[ \t]+|--[\w-]*(?:token|secret|password|passwd|credential|api-key)[\w-]*(?:=|[ \t]+))`)

func (r Redactor) CronText(s string) string {
	var b strings.Builder
	for {
		loc := cronPrivate.FindStringIndex(s)
		if loc == nil {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:loc[1]])
		s = s[loc[1]:]
		end := privateWordEnd(s)
		b.WriteString(Hidden)
		s = s[end:]
	}
	return r.Text(b.String())
}

func privateWordEnd(s string) int {
	var quote byte
	depth := 0
	for n := 0; n < len(s); n++ {
		c := s[n]
		if c == '\\' && quote != '\'' {
			if n+1 < len(s) {
				n++
			}
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			continue
		}
		if c == '(' {
			depth++
			continue
		}
		if c == ')' && depth > 0 {
			depth--
			continue
		}
		if depth == 0 && strings.ContainsRune(" \t\r\n;&|", rune(c)) {
			return n
		}
	}
	return len(s)
}

// CronValues deliberately includes non-sensitive keys, unlike launchd env.
func CronValues(values []string) []string {
	out := []string{}
	for _, v := range values {
		if len(v) >= MinSecretLength {
			out = append(out, v)
		}
	}
	return out
}
