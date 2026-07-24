package cdi

import (
	"testing"
)

func TestUMDMounts_FakeSkips(t *testing.T) {
	// Fake mode has no real driver on the host, so no UMD artifacts mount.
	if ms := umdMounts(true); ms != nil {
		t.Errorf("umdMounts(fake) = %v, want nil", ms)
	}
}

func TestUMDMounts_RealMountsAllUnconditionally(t *testing.T) {
	// Real mode emits every umdHostPath as a host bind mount, with NO existence
	// check: the paths are host paths, but the old os.Stat ran against the
	// calling process's own filesystem — so the same host produced different
	// specs depending on which image (device-plugin vs distroless ha-ctk daemon)
	// generated it. The daemon's stat failed and dropped every mount, yielding a
	// device node with no driver library. The contract is now image-independent.
	saved := umdHostPaths
	umdHostPaths = []string{"/a/libha_driver.so", "/b/ha_driver.h", "/c/x.pc"}
	t.Cleanup(func() { umdHostPaths = saved })

	ms := umdMounts(false)
	if len(ms) != len(umdHostPaths) {
		t.Fatalf("umdMounts(real) returned %d mounts, want %d (all, unconditionally)", len(ms), len(umdHostPaths))
	}
	for i, p := range umdHostPaths {
		if ms[i].HostPath != p || ms[i].ContainerPath != p {
			t.Errorf("mount[%d] = host %q / container %q, want both %q", i, ms[i].HostPath, ms[i].ContainerPath, p)
		}
		want := map[string]bool{"ro": true, "nosuid": true, "nodev": true, "bind": true}
		for _, opt := range ms[i].Options {
			delete(want, opt)
		}
		if len(want) != 0 {
			t.Errorf("mount[%d] options %v missing %v", i, ms[i].Options, want)
		}
	}
}
