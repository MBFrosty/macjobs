package registry

import (
	"path/filepath"
	"testing"
)

func TestMetadataLockReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	unlock, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := Lock(path); err == nil {
		second()
		unlock()
		t.Fatal("concurrent transaction acquired registry lock")
	}
	unlock()
	unlock, err = Lock(path)
	if err != nil {
		t.Fatal("lock was not released", err)
	}
	unlock()
}
