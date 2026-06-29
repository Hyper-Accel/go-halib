package cdi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUMDMounts_FakeSkips(t *testing.T) {
	// Fake mode has no real driver on the host, so no UMD artifacts mount.
	if ms := umdMounts(true); ms != nil {
		t.Errorf("umdMounts(fake) = %v, want nil", ms)
	}
}

func TestUMDMounts_RealMountsOnlyExisting(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "libha_driver.so")
	if err := os.WriteFile(present, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(dir, "missing.so")

	saved := umdHostPaths
	umdHostPaths = []string{present, absent}
	t.Cleanup(func() { umdHostPaths = saved })

	ms := umdMounts(false)
	if len(ms) != 1 {
		t.Fatalf("umdMounts(real) returned %d mounts, want 1 (only the existing artifact)", len(ms))
	}
	if ms[0].HostPath != present || ms[0].ContainerPath != present {
		t.Errorf("mount = host %q / container %q, want both %q", ms[0].HostPath, ms[0].ContainerPath, present)
	}
	want := map[string]bool{"ro": true, "nosuid": true, "nodev": true, "bind": true}
	for _, opt := range ms[0].Options {
		delete(want, opt)
	}
	if len(want) != 0 {
		t.Errorf("mount options %v missing %v", ms[0].Options, want)
	}
}
