package registry

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = 1
const DefaultLabelPrefix = "local.macjobs."

type Match struct {
	Name  string `yaml:"name,omitempty" json:"name,omitempty"`
	Regex string `yaml:"regex,omitempty" json:"regex,omitempty"`
	Port  int    `yaml:"port,omitempty" json:"port,omitempty"`
}
type Entry struct {
	Tags          []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	ID            string   `yaml:"id" json:"id"`
	Kind          string   `yaml:"kind,omitempty" json:"kind,omitempty"`
	Owner         string   `yaml:"owner" json:"owner"`
	Purpose       string   `yaml:"purpose,omitempty" json:"purpose,omitempty"`
	Managed       bool     `yaml:"managed" json:"managed"`
	CreatedByTool bool     `yaml:"created_by_tool" json:"created_by_tool"`
	CreatedBy     string   `yaml:"created_by,omitempty" json:"created_by,omitempty"`
	Template      string   `yaml:"template,omitempty" json:"template,omitempty"`
	AdoptedAt     string   `yaml:"adopted_at,omitempty" json:"adopted_at,omitempty"`
	Port          int      `yaml:"port,omitempty" json:"port,omitempty"`
	Ports         []int    `yaml:"ports,omitempty" json:"ports,omitempty"`
	Log           string   `yaml:"log,omitempty" json:"log,omitempty"`
	Match         *Match   `yaml:"match,omitempty" json:"match,omitempty"`
}
type Registry struct {
	SchemaVersion int     `yaml:"schemaVersion" json:"schemaVersion"`
	LabelPrefix   string  `yaml:"label_prefix,omitempty" json:"label_prefix,omitempty"`
	Entries       []Entry `yaml:"entries" json:"entries"`
}

func Empty() Registry { return Registry{SchemaVersion: SchemaVersion, Entries: []Entry{}} }
func DefaultPath() (string, error) {
	h, e := os.UserHomeDir()
	return filepath.Join(h, ".config", "macjobs", "registry.yaml"), e
}
func (r Registry) ManagedLabelPrefix() string {
	if r.LabelPrefix != "" {
		return r.LabelPrefix
	}
	return DefaultLabelPrefix
}
func Validate(r Registry) error {
	if r.LabelPrefix != "" && !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*\.$`).MatchString(r.LabelPrefix) {
		return errors.New("label_prefix must be a label prefix ending in a dot")
	}
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported registry schemaVersion (expected %d)", SchemaVersion)
	}
	ids := map[string]bool{}
	for _, e := range r.Entries {
		if strings.TrimSpace(e.ID) == "" || ids[e.ID] {
			return errors.New("registry IDs must be nonempty and unique")
		}
		ids[e.ID] = true
		for _, tag := range e.Tags {
			if !validTag.MatchString(tag) {
				return errors.New("tags must match [a-z0-9][a-z0-9._-]{0,31}")
			}
		}
		if e.Port < 0 || e.Port > 65535 {
			return errors.New("registry port must be between 1 and 65535 when specified")
		}
		if _, err := NormalizePorts(e.Ports); err != nil {
			return err
		}
		switch e.Kind {
		case "", "launchd", "system", "brew", "tailscale", "process", "cron":
		default:
			return errors.New("unknown registry kind")
		}
		if e.Match != nil {
			if e.Match.Port < 0 || e.Match.Port > 65535 {
				return errors.New("match port is out of range")
			}
			if e.Match.Name == "" && e.Match.Regex == "" && e.Match.Port == 0 {
				return errors.New("match must specify name, regex, or port")
			}
			if e.Match.Regex != "" {
				if _, err := regexp.Compile(e.Match.Regex); err != nil {
					return errors.New("invalid match regex")
				}
			}
		}
	}
	return nil
}
func Load(path string) (Registry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Empty(), nil
	}
	if err != nil {
		return Empty(), errors.New("cannot read registry")
	}
	defer f.Close()
	var r Registry
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil {
		return Empty(), errors.New("invalid registry YAML")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Empty(), errors.New("registry must contain one YAML document")
	}
	if err := Validate(r); err != nil {
		return Empty(), err
	}
	return normalizeEntries(r), nil
}
func Save(path string, r Registry, force bool) error {
	if err := Validate(r); err != nil {
		return err
	}
	r = normalizeEntries(r)
	b, err := yaml.Marshal(r)
	if err != nil {
		return errors.New("cannot encode registry")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return errors.New("cannot create registry directory")
	}
	f, err := os.CreateTemp(dir, ".registry-*")
	if err != nil {
		return errors.New("cannot create registry temporary file")
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot write registry")
	}
	if force {
		err = os.Rename(tmp, path)
	} else {
		err = os.Link(tmp, path)
	}
	if os.IsExist(err) {
		return errors.New("registry already exists; use --force to replace it")
	}
	if err != nil {
		return errors.New("cannot publish registry atomically")
	}
	return nil
}

var validTag = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// NormalizeTags lowercases and validates tokens, returning a sorted, deduplicated slice.
// Invalid input is never included in errors, since it may contain credentials.
func NormalizeTags(tags []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range tags {
		tag := strings.ToLower(raw)
		if !validTag.MatchString(tag) {
			return nil, errors.New("tags must match [a-z0-9][a-z0-9._-]{0,31}")
		}
		if !seen[tag] {
			out = append(out, tag)
			seen[tag] = true
		}
	}
	sort.Strings(out)
	return out, nil
}

func NormalizePorts(ports []int) ([]int, error) {
	out := []int{}
	seen := map[int]bool{}
	for _, port := range ports {
		if port < 1 || port > 65535 {
			return nil, errors.New("registry ports must be between 1 and 65535")
		}
		if !seen[port] {
			out = append(out, port)
			seen[port] = true
		}
	}
	sort.Ints(out)
	return out, nil
}

func normalizeEntries(r Registry) Registry {
	if r.Entries != nil {
		entries := make([]Entry, len(r.Entries))
		copy(entries, r.Entries)
		r.Entries = entries
	}
	for n := range r.Entries {
		if len(r.Entries[n].Tags) > 0 {
			r.Entries[n].Tags, _ = NormalizeTags(r.Entries[n].Tags)
		}
		if len(r.Entries[n].Ports) > 0 {
			r.Entries[n].Ports, _ = NormalizePorts(append([]int(nil), r.Entries[n].Ports...))
		}
	}
	return r
}
