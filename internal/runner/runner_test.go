package runner

import (
	"context"
	"testing"
)

func TestAllowlist(t *testing.T) {
	for _, args := range [][]string{{"launchctl", "bootstrap", "gui/501", "job"}, {"launchctl", "kickstart", "gui/501/job"}, {"launchctl", "remove", "job"}, {"sudo", "launchctl", "list"}, {"brew", "services", "restart", "example"}, {"tailscale", "serve", "reset"}, {"tailscale", "funnel", "8080"}, {"sh", "-c", "echo example"}} {
		if Allowed(args[0], args[1:]) {
			t.Fatal("mutation allowed", args)
		}
		if _, e := (OS{}).Run(context.Background(), args[0], args[1:]...); e == nil {
			t.Fatal("mutation not rejected")
		}
	}
	for _, args := range [][]string{{"launchctl", "print", "gui/501/job"}, {"launchctl", "print-disabled", "system"}, {"launchctl", "list"}, {"brew", "services", "list", "--json"}, {"tailscale", "serve", "get-config", "--all"}, {"lsof", "-nP", "-iTCP", "-sTCP:LISTEN"}, {"lsof", "-nP", "-iUDP"}, {"ps", "-axo", "pid=,comm="}, {"ps", "-axo", "pid=,ppid=,comm="}} {
		if !Allowed(args[0], args[1:]) {
			t.Fatal("read denied", args)
		}
	}
	for _, args := range [][]string{{"lsof", "-nP", "-iUDP", "-sUDP:IDLE"}, {"lsof", "-iUDP"}, {"lsof", "-nP", "-iTCP"}, {"ps", "-axo", "pid=,ppid=,comm=,args="}, {"ps", "-axo", "pid=,ppid="}, {"netstat", "-anv", "-p", "tcp"}} {
		if Allowed(args[0], args[1:]) {
			t.Fatal("near-miss allowed", args)
		}
	}
}

func TestLifecycleAllowlist(t *testing.T) {
	for _, args := range [][]string{
		{"launchctl", "kickstart", "gui/501/com.example.worker"},
		{"launchctl", "kickstart", "-k", "gui/501/com.example.worker"},
		{"launchctl", "bootstrap", "gui/501", "/home/example/Library/LaunchAgents/com.example.worker.plist"},
		{"launchctl", "bootout", "gui/501/com.example.worker"},
		{"launchctl", "enable", "gui/501/com.example.worker"},
		{"launchctl", "disable", "gui/501/com.example.worker"},
		{"brew", "services", "restart", "example@1"},
		{"plutil", "-lint", "/tmp/example.plist"},
	} {
		if !LifecycleAllowed(args[0], args[1:]) {
			t.Fatal("allowed action denied", args)
		}
	}
	for _, args := range [][]string{
		{"sudo", "launchctl", "list"}, {"launchctl", "load", "/tmp/job.plist"}, {"launchctl", "unload", "/tmp/job.plist"},
		{"launchctl", "kickstart", "system/com.example.worker"}, {"launchctl", "bootout", "gui/501/com.apple.example"},
		{"launchctl", "bootstrap", "gui/501", "/System/Library/LaunchAgents/example.plist"},
		{"launchctl", "bootstrap", "gui/501", "/Library/LaunchDaemons/example.plist"},
		{"launchctl", "bootstrap", "gui/501", "/Library/LaunchAgents/com.apple.example.plist"},
		{"launchctl", "kickstart", "gui/501/-example"}, {"launchctl", "kickstart", "gui/501/../system"},
		{"brew", "services", "restart", "--all"}, {"brew", "services", "restart", "example", "--sudo"},
		{"plutil", "-convert", "xml1", "/tmp/example.plist"}, {"sh", "-c", "echo example"},
	} {
		if LifecycleAllowed(args[0], args[1:]) {
			t.Fatal("unsafe action allowed", args)
		}
		if _, err := (Lifecycle{}).Run(context.Background(), args[0], args[1:]...); err == nil {
			t.Fatal("unsafe action executed", args)
		}
	}
}
func TestParentQueryAllowlist(t *testing.T) {
	if !Allowed("ps", []string{"-o", "ppid=", "-p", "100"}) {
		t.Fatal("parent query refused")
	}
	for _, pid := range []string{"", "-1", "all", "1; echo example", "01"} {
		if Allowed("ps", []string{"-o", "ppid=", "-p", pid}) {
			t.Fatal(pid)
		}
	}
}
func TestEditorCannotEscalate(t *testing.T) {
	if _, err := (Lifecycle{Editor: []string{"sudo"}}).Run(context.Background(), "sudo", "/tmp/example.plist"); err == nil {
		t.Fatal("sudo editor allowed")
	}
}

func TestWatchdogAllowlist(t *testing.T) {
	for _, args := range [][]string{{"open", "-a", "Example App"}, {"pgrep", "-x", "Example App"}, {"ps", "-axo", "pid=,comm="}, {"lsof", "-nP", "-a", "-p", "42", "-iTCP", "-sTCP:ESTABLISHED", "-Fn"}} {
		if !WatchdogAllowed(args[0], args[1:]) {
			t.Fatal("watchdog read denied", args)
		}
	}
	for _, args := range [][]string{{"open", "-a", "--background"}, {"open", "-a", "/Applications/Example.app"}, {"open", "-a", "Example App", "--args", "--private"}, {"pgrep", "-f", "Example"}, {"lsof", "-nP", "-a", "-p", "-1", "-iTCP", "-sTCP:ESTABLISHED", "-Fn"}, {"kill", "42"}, {"launchctl", "kickstart", "gui/501/com.example.worker"}} {
		if WatchdogAllowed(args[0], args[1:]) {
			t.Fatal("unsafe watchdog command allowed", args)
		}
		if _, err := (Watchdog{}).Run(context.Background(), args[0], args[1:]...); err == nil {
			t.Fatal("unsafe watchdog command executed")
		}
	}
}

func TestWatchdogQuitGate(t *testing.T) {
	script := WatchdogQuitScript("Example App")
	if !(Watchdog{QuitApp: "Example App"}).Allowed("osascript", []string{"-e", script}) {
		t.Fatal("scoped quit denied")
	}
	for _, w := range []Watchdog{{}, {QuitApp: "Other App"}} {
		if _, err := w.Run(context.Background(), "osascript", "-e", script); err == nil {
			t.Fatal("quit without matching opt-in")
		}
	}
	for _, script := range []string{`tell application "Example App" to quit`, WatchdogQuitScript("Other App"), "do shell script \"echo private\""} {
		if _, err := (Watchdog{QuitApp: "Example App"}).Run(context.Background(), "osascript", "-e", script); err == nil {
			t.Fatal("arbitrary script allowed")
		}
	}
	if got := WatchdogQuitScript(`Example "quoted" \ App`); got != `if application "Example \"quoted\" \\ App" is running then
 tell application "Example \"quoted\" \\ App" to quit
end if` {
		t.Fatal(got)
	}
}

func TestCronAllowlist(t *testing.T) {
	for _, name := range []string{"crontab", "/usr/bin/crontab"} {
		if !Allowed(name, []string{"-l"}) {
			t.Fatal("cron read refused")
		}
	}
	for _, args := range [][]string{nil, {"-e"}, {"-r"}, {"-u", "alice", "-l"}, {"file"}, {"-l", "file"}, {"-l "}, {"-l", ""}, {"--list"}} {
		if Allowed("crontab", args) {
			t.Fatal("cron near miss allowed", args)
		}
		if _, err := (OS{}).Run(context.Background(), "crontab", args...); err == nil {
			t.Fatal("unsafe command not refused")
		}
		if LifecycleAllowed("crontab", args) {
			t.Fatal("cron mutation path allowed")
		}
	}
}

func TestExecutableQueryAllowlist(t *testing.T) {
	for _, pids := range []string{"42", "42,73"} {
		if !Allowed("lsof", []string{"-nP", "-a", "-p", pids, "-d", "txt", "-Fpn"}) {
			t.Fatal(pids)
		}
	}
	for _, pids := range []string{"", "-1", "0", "all", "42,", "42,-73", "042", "42 -u root"} {
		if Allowed("lsof", []string{"-nP", "-a", "-p", pids, "-d", "txt", "-Fpn"}) {
			t.Fatal(pids)
		}
	}
	if Allowed("lsof", []string{"-nP", "-a", "-p", "42", "-d", "cwd", "-Fpn"}) {
		t.Fatal("non-text mapping allowed")
	}
}
