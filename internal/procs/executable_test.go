package procs

import "testing"

func TestAppleExecutable(t *testing.T) {
	for _, path := range []string{"/System/Library/CoreServices/ControlCenter.app/Contents/MacOS/ControlCenter", "/System/Cryptexes/App/usr/libexec/example", "/usr/libexec/rapportd", "/usr/sbin/example", "/Library/Apple/usr/libexec/example"} {
		if !AppleExecutable(path) {
			t.Errorf("not classified: %s", path)
		}
	}
	for _, path := range []string{"", "rapportd", "System/example", "/Applications/Example.app/Contents/MacOS/Example", "/usr/local/bin/example", "/Systematic/example", "/usr/libexec-extra/example", "/Library/AppleExtra/example", "/System/../Applications/Example.app/Contents/MacOS/Example"} {
		if AppleExecutable(path) {
			t.Errorf("incorrectly classified: %s", path)
		}
	}
}

func TestParseExecutables(t *testing.T) {
	paths := ParseExecutables([]byte("n/System/orphan\np42\nn/Applications/Example App.app/Contents/MacOS/Example App\nn/System/Library/Frameworks/Example\np43\nn/usr/libexec/rapportd\npbad\nn/System/invalid\n"))
	if len(paths) != 2 || paths[42] != "/Applications/Example App.app/Contents/MacOS/Example App" || paths[43] != "/usr/libexec/rapportd" {
		t.Fatal(paths)
	}
}
