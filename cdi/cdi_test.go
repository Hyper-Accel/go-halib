package cdi

import (
	"fmt"
	"testing"

	"github.com/Hyper-Accel/go-halib/types"
	cdiSpecs "tags.cncf.io/container-device-interface/specs-go"
)

func TestExtractDeviceIndex(t *testing.T) {
	tests := []struct {
		name     string
		pciAddr  string
		expected int
	}{
		{"bus 01 -> index 0", "0000:01:00.0", 0},
		{"bus 02 -> index 1", "0000:02:00.0", 1},
		{"bus 03 -> index 2", "0000:03:00.0", 2},
		{"malformed -> 0", "garbage", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractDeviceIndex(tt.pciAddr); got != tt.expected {
				t.Errorf("ExtractDeviceIndex(%s) = %d, want %d", tt.pciAddr, got, tt.expected)
			}
		})
	}
}

func TestGenerateSpec(t *testing.T) {
	devices := []*types.Device{
		{ID: "1f26-0000:01:00.0", PCIAddr: "0000:01:00.0", VendorID: "1f26", DeviceID: "0000", Health: types.Healthy},
		{ID: "1f26-0000:02:00.0", PCIAddr: "0000:02:00.0", VendorID: "1f26", DeviceID: "0000", Health: types.Healthy},
	}

	spec, err := GenerateSpec(devices, DefaultVendor, DefaultClass, true, "")
	if err != nil {
		t.Fatalf("GenerateSpec() error = %v", err)
	}

	if spec.Version != DefaultVersion {
		t.Errorf("Version = %s, want %s", spec.Version, DefaultVersion)
	}
	expectedKind := fmt.Sprintf("%s/%s", DefaultVendor, DefaultClass)
	if spec.Kind != expectedKind {
		t.Errorf("Kind = %s, want %s", spec.Kind, expectedKind)
	}
	// Each device gets a human-friendly index alias + a stable BDF alias, plus a
	// single "all". 2 devices -> "0","0000_01_00.0","1","0000_02_00.0","all" = 5.
	if len(spec.Devices) != 5 {
		t.Fatalf("Devices count = %d, want 5 (2 index + 2 bdf + all)", len(spec.Devices))
	}
	byName := map[string]cdiSpecs.Device{}
	for _, d := range spec.Devices {
		byName[d.Name] = d
	}
	// Index alias "0" and BDF alias "0000_01_00.0" both map to the same /dev/ha0.
	for _, name := range []string{"0", "0000_01_00.0"} {
		d, ok := byName[name]
		if !ok {
			t.Fatalf("missing device alias %q", name)
		}
		if len(d.ContainerEdits.DeviceNodes) != 1 {
			t.Fatalf("alias %q DeviceNodes = %d, want 1", name, len(d.ContainerEdits.DeviceNodes))
		}
		if got := d.ContainerEdits.DeviceNodes[0].Path; got != "/dev/ha0" {
			t.Errorf("alias %q Path = %s, want /dev/ha0", name, got)
		}
		if got := d.ContainerEdits.DeviceNodes[0].HostPath; got != "/tmp/fake-devices/dev/ha0" {
			t.Errorf("alias %q HostPath = %s, want /tmp/fake-devices/dev/ha0", name, got)
		}
	}
	// The second device's aliases exist too.
	for _, name := range []string{"1", "0000_02_00.0"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("missing device alias %q", name)
		}
	}
	// "all" injects every device node.
	all, ok := byName["all"]
	if !ok {
		t.Fatal(`missing "all" device`)
	}
	if len(all.ContainerEdits.DeviceNodes) != 2 {
		t.Errorf("all DeviceNodes = %d, want 2", len(all.ContainerEdits.DeviceNodes))
	}
}

func TestGenerateSpec_CustomKind(t *testing.T) {
	// Consumers inject the CDI kind; device-plugin passes "bertha" for wire compat.
	devices := []*types.Device{{ID: "x", PCIAddr: "0000:01:00.0", VendorID: "1f26", DeviceID: "0000"}}
	spec, err := GenerateSpec(devices, "hyperaccel.ai", "bertha", true, "")
	if err != nil {
		t.Fatalf("GenerateSpec() error = %v", err)
	}
	if spec.Kind != "hyperaccel.ai/bertha" {
		t.Errorf("Kind = %s, want hyperaccel.ai/bertha", spec.Kind)
	}
}

func TestGenerateSpec_Empty(t *testing.T) {
	if _, err := GenerateSpec(nil, DefaultVendor, DefaultClass, true, ""); err == nil {
		t.Error("GenerateSpec(nil) should error")
	}
}

// A non-empty fakeBaseDir must root the fake host path (so it matches where the
// fake discoverer created the node), not the hard-coded default. bus 0x81 -> 128.
func TestGenerateSpec_CustomFakeBaseDir(t *testing.T) {
	devices := []*types.Device{{ID: "x", PCIAddr: "0000:81:00.0", VendorID: "1f26"}}
	spec, err := GenerateSpec(devices, DefaultVendor, DefaultClass, true, "/custom/fakes")
	if err != nil {
		t.Fatalf("GenerateSpec: %v", err)
	}
	if got, want := spec.Devices[0].ContainerEdits.DeviceNodes[0].HostPath, "/custom/fakes/dev/ha128"; got != want {
		t.Errorf("HostPath = %s, want %s (fakeBaseDir must root the host path)", got, want)
	}
}

// createDeviceSpec: real mode prefers the driver-registered DevName ("ha0")
// over the legacy PCI-bus-derived index, because drivers allocate /dev/haN in
// probe order — the bus-derived path collides on multi-LPU hosts.
func TestCreateDeviceSpec_RealUsesDevName(t *testing.T) {
	dev := &types.Device{ID: "20fb-0000:19:00.0", PCIAddr: "0000:19:00.0", VendorID: "20fb", DeviceID: "0010", Health: types.Healthy, DevName: "ha0"}
	spec, err := createDeviceSpec(dev, false, "")
	if err != nil {
		t.Fatalf("createDeviceSpec: %v", err)
	}
	node := spec.ContainerEdits.DeviceNodes[0]
	if node.HostPath != "/dev/ha0" {
		t.Errorf("HostPath = %s, want /dev/ha0 (DevName must beat PCI-bus index)", node.HostPath)
	}
	if node.Path != "/dev/ha0" {
		t.Errorf("Path = %s, want /dev/ha0", node.Path)
	}
}

// Empty DevName falls back to the PCI-bus derivation (safe for drivers with no
// sysfs class symlink, and for fixtures).
func TestCreateDeviceSpec_RealFallbackWithoutDevName(t *testing.T) {
	dev := &types.Device{PCIAddr: "0000:01:00.0"} // bus 1 -> index 0
	spec, err := createDeviceSpec(dev, false, "")
	if err != nil {
		t.Fatalf("createDeviceSpec: %v", err)
	}
	if got := spec.ContainerEdits.DeviceNodes[0].HostPath; got != "/dev/ha0" {
		t.Errorf("HostPath = %s, want /dev/ha0 (fallback)", got)
	}
}

// Fake mode never consumes DevName, preserving the /tmp/fake-devices layout CI relies on.
func TestCreateDeviceSpec_FakeIgnoresDevName(t *testing.T) {
	dev := &types.Device{PCIAddr: "0000:01:00.0", DevName: "should-not-be-used"}
	spec, err := createDeviceSpec(dev, true, "/tmp/fake-devices")
	if err != nil {
		t.Fatalf("createDeviceSpec: %v", err)
	}
	if got := spec.ContainerEdits.DeviceNodes[0].HostPath; got != "/tmp/fake-devices/dev/ha0" {
		t.Errorf("HostPath = %s, want /tmp/fake-devices/dev/ha0", got)
	}
}
