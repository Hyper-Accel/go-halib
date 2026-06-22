package cdi

import (
	"fmt"
	"testing"

	"github.com/Hyper-Accel/go-halib/types"
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

	spec, err := GenerateSpec(devices, DefaultVendor, DefaultClass, true)
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
	if len(spec.Devices) != 2 {
		t.Fatalf("Devices count = %d, want 2", len(spec.Devices))
	}

	dev0 := spec.Devices[0]
	if dev0.Name != "0000_01_00.0" {
		t.Errorf("Device[0].Name = %s, want 0000_01_00.0", dev0.Name)
	}
	if len(dev0.ContainerEdits.DeviceNodes) != 1 {
		t.Fatalf("Device[0].DeviceNodes count = %d, want 1", len(dev0.ContainerEdits.DeviceNodes))
	}
	if got := dev0.ContainerEdits.DeviceNodes[0].Path; got != "/dev/ha0" {
		t.Errorf("Device[0].DeviceNodes[0].Path = %s, want /dev/ha0", got)
	}
	if got := dev0.ContainerEdits.DeviceNodes[0].HostPath; got != "/tmp/fake-devices/dev/ha0" {
		t.Errorf("Device[0].DeviceNodes[0].HostPath = %s, want /tmp/fake-devices/dev/ha0", got)
	}
}

func TestGenerateSpec_CustomKind(t *testing.T) {
	// Consumers inject the CDI kind; device-plugin passes "bertha" for wire compat.
	devices := []*types.Device{{ID: "x", PCIAddr: "0000:01:00.0", VendorID: "1f26", DeviceID: "0000"}}
	spec, err := GenerateSpec(devices, "hyperaccel.ai", "bertha", true)
	if err != nil {
		t.Fatalf("GenerateSpec() error = %v", err)
	}
	if spec.Kind != "hyperaccel.ai/bertha" {
		t.Errorf("Kind = %s, want hyperaccel.ai/bertha", spec.Kind)
	}
}

func TestGenerateSpec_Empty(t *testing.T) {
	if _, err := GenerateSpec(nil, DefaultVendor, DefaultClass, true); err == nil {
		t.Error("GenerateSpec(nil) should error")
	}
}

// createDeviceSpec: real mode prefers the driver-registered DevName ("ha0")
// over the legacy PCI-bus-derived index, because drivers allocate /dev/haN in
// probe order — the bus-derived path collides on multi-LPU hosts.
func TestCreateDeviceSpec_RealUsesDevName(t *testing.T) {
	dev := &types.Device{ID: "20fb-0000:19:00.0", PCIAddr: "0000:19:00.0", VendorID: "20fb", DeviceID: "0010", Health: types.Healthy, DevName: "ha0"}
	spec, err := createDeviceSpec(dev, false)
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
	spec, err := createDeviceSpec(dev, false)
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
	spec, err := createDeviceSpec(dev, true)
	if err != nil {
		t.Fatalf("createDeviceSpec: %v", err)
	}
	if got := spec.ContainerEdits.DeviceNodes[0].HostPath; got != "/tmp/fake-devices/dev/ha0" {
		t.Errorf("HostPath = %s, want /tmp/fake-devices/dev/ha0", got)
	}
}
