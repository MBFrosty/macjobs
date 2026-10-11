package redact

import (
	"regexp"
	"sort"
	"strings"
)

const Hidden = "[REDACTED]"

var sensitive = regexp.MustCompile(`(?i)(token|secret|password|passwd|authorization|credential|api.?key|private.?key|cookie)`)
var pairs = regexp.MustCompile(`(?i)((?:[\w-]*(?:token|secret|password|passwd|authorization|credential|api[_-]?key|cookie)[\w-]*)["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;}]+)`)
var bearer = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[A-Za-z0-9_./+=:-]+`)
var headers = regexp.MustCompile(`(?im)((?:authorization|proxy-authorization|cookie|set-cookie)\s*:\s*)[^\r\n]+`)
var userinfo = regexp.MustCompile(`(https?://)[^\s/@]+:[^\s/@]+@`)
var options = regexp.MustCompile(`(?i)(--[\w-]*(?:token|secret|password|passwd|credential|api-key)[\w-]*\s+)("[^"]*"|'[^']*'|\S+)`)
var jwt = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
var opaque = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9_]+|github_pat_[A-Za-z0-9_]+|tskey-[A-Za-z0-9_-]+|sk-[A-Za-z0-9_-]{12,})\b`)

func Sensitive(key string) bool { return sensitive.MatchString(key) }

type Redactor struct{ Values []string }

func (r Redactor) Text(s string) string {
	values := append([]string(nil), r.Values...)
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, v := range values {
		if v != "" {
			s = strings.ReplaceAll(s, v, Hidden)
		}
	}
	s = headers.ReplaceAllString(s, "${1}"+Hidden)
	s = pairs.ReplaceAllString(s, "${1}"+Hidden)
	s = bearer.ReplaceAllString(s, "${1} "+Hidden)
	s = userinfo.ReplaceAllString(s, "${1}"+Hidden+"@")
	s = options.ReplaceAllString(s, "${1}"+Hidden)
	s = jwt.ReplaceAllString(s, Hidden)
	return opaque.ReplaceAllString(s, Hidden)
}
func (r Redactor) Value(v any, showEnv bool) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			if k == "EnvironmentVariables" {
				if m, ok := v.(map[string]any); ok {
					env := map[string]any{}
					for name, value := range m {
						if !showEnv || Sensitive(name) {
							env[name] = Hidden
						} else {
							env[name] = r.Value(value, showEnv)
						}
					}
					out[k] = env
					continue
				}
			}
			if Sensitive(k) {
				out[k] = Hidden
			} else {
				out[k] = r.Value(v, showEnv)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		hideNext := false
		for i, v := range x {
			if hideNext {
				out[i] = Hidden
				hideNext = false
				continue
			}
			out[i] = r.Value(v, showEnv)
			if s, ok := v.(string); ok && strings.HasPrefix(s, "-") && Sensitive(s) && !strings.Contains(s, "=") {
				hideNext = true
			}
		}
		return out
	case string:
		return r.Text(x)
	default:
		return v
	}
}
func AllEnvironmentValues(p map[string]any) []string {
	out := []string{}
	if m, ok := p["EnvironmentVariables"].(map[string]any); ok {
		for _, v := range m {
			if s, ok := v.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func EnvironmentValues(p map[string]any) []string {
	out := []string{}
	if m, ok := p["EnvironmentVariables"].(map[string]any); ok {
		for key, v := range m {
			if s, ok := v.(string); ok && s != "" && Sensitive(key) {
				out = append(out, s)
			}
		}
	}
	return out
}
