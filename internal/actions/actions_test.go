package actions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/registry"
)

type fakeRunner struct {
	calls   []Operation
	loaded  bool
	pid     int
	parents map[string]string
	hook    func(string, []string) error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, Operation{Command: name, Args: append([]string{}, args...)})
	if f.hook != nil {
		if err := f.hook(name, args); err != nil {
			return nil, err
		}
	}
	if name == "ps" {
		parent, ok := f.parents[args[3]]
		if !ok {
			return nil, errors.New("missing replay")
		}
		return []byte(parent), nil
	}
	if name == "launchctl" && args[0] == "print" {
		if strings.Count(args[1], "/") == 1 {
			return []byte("gui/501 = {\n services = {\n }\n}\n"), nil
		}
		if !f.loaded {
			return nil, errors.New("not loaded")
		}
		return []byte("gui/501/com.example.worker = {\n state = running\n pid = " + itoa(f.pid) + "\n}\n"), nil
	}
	return nil, nil
}
func itoa(n int) string { return strconv.Itoa(n) }
func setup(t *testing.T) (Service, model.Item, *fakeRunner) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "com.example.worker.plist")
	if err := os.WriteFile(path, plistBytes(60), 0640); err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(home, ".config", "macjobs", "registry.yaml")
	reg := registry.Empty()
	reg.LabelPrefix = "local.example."
	reg.Entries = []registry.Entry{{ID: "com.example.worker", Kind: "launchd", Managed: true, CreatedByTool: true}, {ID: "com.example.other", Owner: "team"}}
	if err := registry.Save(registryPath, reg, false); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{loaded: true, parents: map[string]string{"100": "90", "90": "1"}}
	s := Service{Runner: f, Home: home, UID: 501, PID: 100, RegistryPath: registryPath, Editor: []string{"example-editor", "--wait"}, Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }}
	i := model.Item{ID: "com.example.worker", Label: "com.example.worker", Kind: "launchd", Domain: "gui/501", PlistPath: path, Registered: true, Managed: true, CreatedByTool: true, Live: true}
	return s, i, f
}
func plistBytes(interval int) []byte {
	return []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>Label</key><string>com.example.worker</string><key>ProgramArguments</key><array><string>/bin/echo</string><string>example</string></array><key>StartInterval</key><integer>` + strconv.Itoa(interval) + `</integer></dict></plist>`)
}
func TestForeignSafety(t *testing.T) {
	for _, action := range []string{"stop", "restart", "disable", "remove", "edit"} {
		for _, mode := range []string{"unregistered", "unmanaged", "not-created"} {
			for _, gate := range []string{"no-yes", "no-tty", "mismatch", "confirmed"} {
				t.Run(action+"/"+mode+"/"+gate, func(t *testing.T) {
					s, i, f := setup(t)
					switch mode {
					case "unregistered":
						i.Registered = false
					case "unmanaged":
						i.Managed = false
					case "not-created":
						i.CreatedByTool = false
					}
					s.TTY = gate != "no-tty"
					s.Confirm = func(string) (string, error) {
						if gate == "mismatch" {
							return "other", nil
						}
						return i.Label + "\n", nil
					}
					r, err := s.Execute(context.Background(), action, i, Options{Yes: gate != "no-yes"})
					if gate == "confirmed" {
						if err != nil {
							t.Fatal(r, err)
						}
						return
					}
					if err == nil || r.Error == nil {
						t.Fatal("gate accepted", r)
					}
					if len(f.calls) != 0 {
						t.Fatal("refusal ran a subprocess", f.calls)
					}
				})
			}
		}
	}
}
func TestAncestorGuard(t *testing.T) {
	for _, action := range []string{"stop", "restart", "disable", "remove", "edit"} {
		for _, pid := range []int{100, 90, 1} {
			s, i, f := setup(t)
			i.PID = pid
			r, err := s.Execute(context.Background(), action, i, Options{Yes: true})
			if err == nil || r.Error.Code != "self_harm" {
				t.Fatal(action, pid, r, err)
			}
			for _, call := range f.calls {
				if call.Command != "ps" {
					t.Fatal("mutation during guard", call)
				}
			}
		}
	}
	s, i, f := setup(t)
	f.pid = 90
	r, err := s.Execute(context.Background(), "restart", i, Options{})
	if err == nil || r.Error.Code != "self_harm" {
		t.Fatal("fresh PID was not guarded", r, err)
	}
	s, i, f = setup(t)
	i.PID = 50
	f.parents["90"] = "invalid"
	if _, err := s.Execute(context.Background(), "stop", i, Options{}); err == nil {
		t.Fatal("ancestry failure did not fail closed")
	}
}
func TestDryRunAllActions(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart", "enable", "disable", "edit", "remove"} {
		t.Run(action, func(t *testing.T) {
			s, i, f := setup(t)
			i.Managed, i.CreatedByTool, i.Registered = false, false, false
			i.PID = s.PID
			if action == "start" || action == "enable" {
				i.Live = false
			}
			before, _ := os.ReadFile(i.PlistPath)
			regBefore, _ := os.ReadFile(s.RegistryPath)
			r, err := s.Execute(context.Background(), action, i, Options{DryRun: true})
			after, _ := os.ReadFile(i.PlistPath)
			regAfter, _ := os.ReadFile(s.RegistryPath)
			entries, _ := os.ReadDir(filepath.Dir(i.PlistPath))
			if err != nil || len(f.calls) != 0 || len(r.Ops) == 0 || r.Result != "planned" || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(regBefore, regAfter) || len(entries) != 1 {
				t.Fatal(r, err, f.calls)
			}
			if _, err := os.Stat(filepath.Join(s.Home, ".config", "macjobs", "trash")); !os.IsNotExist(err) {
				t.Fatal("dry run created trash")
			}
		})
	}
}
func TestProtectedJobs(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart", "enable", "disable", "edit", "remove"} {
		for _, protection := range []string{"system", "system-domain", "system-id", "apple", "system-path", "daemon-path", "tailscale", "process", "wrong-user"} {
			for _, dry := range []bool{false, true} {
				s, i, f := setup(t)
				switch protection {
				case "system":
					i.Kind = "system"
				case "system-domain":
					i.Domain = "system"
				case "system-id":
					i.ID = "system:" + i.ID
				case "apple":
					i.Label = "com.apple.example"
				case "system-path":
					i.PlistPath = "/System/Library/LaunchAgents/com.example.worker.plist"
				case "daemon-path":
					i.PlistPath = "/Library/LaunchDaemons/com.example.worker.plist"
				case "tailscale":
					i.Kind = "tailscale"
				case "process":
					i.Kind = "process"
				case "wrong-user":
					i.Domain = "gui/502"
				}
				if _, err := s.Execute(context.Background(), action, i, Options{DryRun: dry, Yes: true}); err == nil {
					t.Fatal(action, protection, dry)
				}
				if len(f.calls) != 0 {
					t.Fatal("protected action called Runner")
				}
			}
		}
	}
}
func TestBrewDelegation(t *testing.T) {
	for _, kind := range []string{"brew", "launchd"} {
		for _, action := range []string{"start", "stop", "restart"} {
			s, i, f := setup(t)
			i.Kind, i.BrewName = kind, "example@1"
			r, err := s.Execute(context.Background(), action, i, Options{})
			if err != nil || len(r.Ops) != 1 || !reflect.DeepEqual(f.calls[len(f.calls)-1], Operation{Command: "brew", Args: []string{"services", action, "example@1"}}) {
				t.Fatal(r, err, f.calls)
			}
		}
	}
	s, i, f := setup(t)
	i.Kind = "brew"
	for _, action := range []string{"enable", "disable", "edit", "remove"} {
		if _, err := s.Execute(context.Background(), action, i, Options{}); err == nil {
			t.Fatal(action)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal(f.calls)
	}
}
func TestLaunchdSemantics(t *testing.T) {
	target := "gui/501/com.example.worker"
	for _, tc := range []struct {
		action string
		loaded bool
		verbs  []string
	}{
		{"start", true, []string{"kickstart"}}, {"start", false, []string{"bootstrap", "kickstart"}},
		{"restart", true, []string{"kickstart"}}, {"restart", false, []string{"bootstrap", "kickstart"}},
		{"stop", true, []string{"bootout"}}, {"stop", false, nil},
		{"disable", true, []string{"disable", "bootout"}}, {"disable", false, []string{"disable"}},
		{"enable", true, []string{"enable"}}, {"enable", false, []string{"enable", "bootstrap"}},
	} {
		s, i, f := setup(t)
		f.loaded = tc.loaded
		r, err := s.Execute(context.Background(), tc.action, i, Options{})
		if err != nil {
			t.Fatal(tc, r, err)
		}
		var verbs []string
		for _, op := range r.Ops {
			verbs = append(verbs, op.Args[0])
			if op.Args[0] == "kickstart" {
				want := []string{"kickstart", target}
				if tc.action == "restart" {
					want = []string{"kickstart", "-k", target}
				}
				if !reflect.DeepEqual(op.Args, want) {
					t.Fatal(op)
				}
			}
		}
		if !reflect.DeepEqual(verbs, tc.verbs) {
			t.Fatal(tc, r)
		}
	}
}
func TestRemoveRecovery(t *testing.T) {
	s, i, _ := setup(t)
	before, _ := os.ReadFile(i.PlistPath)
	r, err := s.Execute(context.Background(), "remove", i, Options{})
	if err != nil {
		t.Fatal(r, err)
	}
	if _, err := os.Stat(i.PlistPath); !os.IsNotExist(err) {
		t.Fatal("original retained")
	}
	trash := r.Ops[2].Args[1]
	recovered, err := os.ReadFile(trash)
	if err != nil || !reflect.DeepEqual(before, recovered) {
		t.Fatal("recovery copy differs", err)
	}
	reg, err := registry.Load(s.RegistryPath)
	if err != nil || len(reg.Entries) != 1 || reg.Entries[0].ID != "com.example.other" || reg.LabelPrefix != "local.example." {
		t.Fatal(reg, err)
	}
	// A collision must retain the original file rather than overwrite the recovery copy.
	if err := os.WriteFile(i.PlistPath, before, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(context.Background(), "remove", i, Options{}); err == nil {
		t.Fatal("overwrote trash collision")
	}
	if _, err := os.Stat(i.PlistPath); err != nil {
		t.Fatal("collision lost original")
	}
}
func TestEditRetainsOriginal(t *testing.T) {
	for _, failure := range []string{"lint", "editor", "label", "symlink", "concurrent", "ancestor"} {
		s, i, f := setup(t)
		before, _ := os.ReadFile(i.PlistPath)
		f.hook = func(name string, args []string) error {
			if name == "example-editor" {
				tmp := args[len(args)-1]
				switch failure {
				case "editor":
					return errors.New("private subprocess failure")
				case "label":
					return os.WriteFile(tmp, []byte(strings.ReplaceAll(string(plistBytes(120)), i.Label, "com.example.changed")), 0600)
				case "symlink":
					if err := os.Remove(tmp); err != nil {
						return err
					}
					return os.Symlink(i.PlistPath, tmp)
				case "concurrent":
					if err := os.WriteFile(i.PlistPath, plistBytes(180), 0640); err != nil {
						return err
					}
				case "ancestor":
					f.pid = 90
				}
				return os.WriteFile(tmp, plistBytes(120), 0600)
			}
			if name == "plutil" && failure == "lint" {
				return errors.New("secret validation failure")
			}
			return nil
		}
		r, err := s.Execute(context.Background(), "edit", i, Options{})
		if err == nil || r.Result != "failed" {
			t.Fatal(failure, r, err)
		}
		after, _ := os.ReadFile(i.PlistPath)
		if failure != "concurrent" && !reflect.DeepEqual(before, after) {
			t.Fatal(failure, "original changed")
		}
		if strings.Contains(r.Error.Message, "secret") || strings.Contains(r.Error.Message, "private subprocess") {
			t.Fatal("raw error exposed")
		}
		for _, call := range f.calls {
			if call.Command == "launchctl" && call.Args[0] != "print" {
				t.Fatal("reloaded invalid plist", call)
			}
		}
		entries, _ := os.ReadDir(filepath.Dir(i.PlistPath))
		if len(entries) != 1 {
			t.Fatal("edit temporary file leaked", entries)
		}
	}
}
func TestEditChangedAndUnchanged(t *testing.T) {
	for _, change := range []bool{false, true} {
		s, i, f := setup(t)
		f.hook = func(name string, args []string) error {
			if name == "example-editor" && change {
				return os.WriteFile(args[len(args)-1], plistBytes(120), 0600)
			}
			return nil
		}
		r, err := s.Execute(context.Background(), "edit", i, Options{})
		if err != nil {
			t.Fatal(r, err)
		}
		after, _ := os.ReadFile(i.PlistPath)
		want := plistBytes(60)
		if change {
			want = plistBytes(120)
		}
		if !reflect.DeepEqual(want, after) {
			t.Fatal("unexpected edited bytes")
		}
		info, _ := os.Stat(i.PlistPath)
		if info.Mode().Perm() != 0640 {
			t.Fatal("mode changed")
		}
		var verbs []string
		for _, call := range f.calls {
			if call.Command == "launchctl" && call.Args[0] != "print" {
				verbs = append(verbs, call.Args[0])
			}
		}
		if change {
			if !reflect.DeepEqual(verbs, []string{"bootout", "bootstrap"}) || r.Result != "completed" {
				t.Fatal(r, verbs)
			}
		} else if len(verbs) != 0 || r.Result != "unchanged" {
			t.Fatal(r, verbs)
		}
		entries, _ := os.ReadDir(filepath.Dir(i.PlistPath))
		if len(entries) != 1 {
			t.Fatal("temp leaked")
		}
	}
}
func TestEditorParsing(t *testing.T) {
	args, err := ParseEditor(`"/Applications/Example Editor/bin/edit" --wait 'an argument'`)
	if err != nil || !reflect.DeepEqual(args, []string{"/Applications/Example Editor/bin/edit", "--wait", "an argument"}) {
		t.Fatal(args, err)
	}
	for _, value := range []string{"", "sudo edit", "'unfinished", `edit \`} {
		if _, err := ParseEditor(value); err == nil {
			t.Fatal(value)
		}
	}
}

func TestMissingAndLinkedPlists(t *testing.T) {
	for _, action := range []string{"start", "restart", "enable", "edit", "remove"} {
		for _, mode := range []string{"missing", "symlink", "label"} {
			s, i, f := setup(t)
			f.loaded, i.Live = false, false
			switch mode {
			case "missing":
				if err := os.Remove(i.PlistPath); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				original := i.PlistPath + ".original"
				if err := os.Rename(i.PlistPath, original); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(original, i.PlistPath); err != nil {
					t.Fatal(err)
				}
			case "label":
				if err := os.WriteFile(i.PlistPath, []byte(strings.ReplaceAll(string(plistBytes(60)), i.Label, "com.example.other")), 0600); err != nil {
					t.Fatal(err)
				}
			}
			r, err := s.Execute(context.Background(), action, i, Options{})
			if err == nil || r.Error.Code != "invalid_plist" {
				t.Fatal(action, mode, r, err)
			}
			for _, call := range f.calls {
				if call.Command != "launchctl" || call.Args[0] != "print" {
					t.Fatal("invalid plist reached mutation", call)
				}
			}
		}
	}
}
func TestBrewFreshAncestorGuard(t *testing.T) {
	s, i, f := setup(t)
	i.Kind, i.BrewName = "brew", "example"
	f.pid = 90
	r, err := s.Execute(context.Background(), "restart", i, Options{})
	if err == nil || r.Error.Code != "self_harm" {
		t.Fatal(r, err)
	}
	for _, call := range f.calls {
		if call.Command == "brew" {
			t.Fatal("brew restarted ancestor")
		}
	}
}
func TestFailureSuppressesSubprocessOutput(t *testing.T) {
	s, i, f := setup(t)
	f.hook = func(name string, args []string) error {
		if name == "launchctl" && args[0] == "kickstart" {
			return errors.New("private-value")
		}
		return nil
	}
	r, err := s.Execute(context.Background(), "restart", i, Options{})
	if err == nil || r.Result != "failed" || r.Error.Code != "operation_failed" || strings.Contains(r.Error.Message, "private-value") {
		t.Fatal(r, err)
	}
}
