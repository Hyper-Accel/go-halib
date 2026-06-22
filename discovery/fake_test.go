package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hyper-Accel/go-halib/types"
)

func TestNewFakeDiscoverer_Count(t *testing.T) {
	d := NewFakeDiscoverer(FakeConfig{Count: 3, BaseDir: t.TempDir(), CreateFiles: false})
	n, err := d.GetDeviceCount()
	if err != nil {
		t.Fatalf("GetDeviceCount: %v", err)
	}
	if n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
	if devs, _ := d.GetDevices(); len(devs) != 3 {
		t.Fatalf("GetDevices len = %d, want 3", len(devs))
	}
}

// Two devices per switch: device 0 and 1 share switch 0 (bus 0x81, 0x82), NUMA 0.
// These are the device-plugin's production fake defaults, so the addresses and
// IDs must stay byte-stable.
func TestNewFakeDiscoverer_Topology(t *testing.T) {
	d := NewFakeDiscoverer(FakeConfig{Count: 2, VendorID: "1eff", DeviceID: "0001", DevicesPerSwitch: 2, BaseDir: t.TempDir()})
	devs, _ := d.GetDevices()
	if devs[0].PCIAddr != "0000:81:00.0" {
		t.Errorf("dev0 PCIAddr = %s, want 0000:81:00.0", devs[0].PCIAddr)
	}
	if devs[1].PCIAddr != "0000:82:00.0" {
		t.Errorf("dev1 PCIAddr = %s, want 0000:82:00.0", devs[1].PCIAddr)
	}
	if devs[0].ID != "1eff-0000:81:00.0" {
		t.Errorf("dev0 ID = %s, want 1eff-0000:81:00.0", devs[0].ID)
	}
	if devs[0].NUMANode != 0 {
		t.Errorf("dev0 NUMANode = %d, want 0", devs[0].NUMANode)
	}
}

func TestFakeDiscoverer_CreateFilesAndSetHealth(t *testing.T) {
	base := t.TempDir()
	d := NewFakeDiscoverer(FakeConfig{Count: 1, BaseDir: base, CreateFiles: true})
	devs, _ := d.GetDevices()
	pciAddr := devs[0].PCIAddr
	devDir := filepath.Join(base, "sys", "bus", "pci", "devices", pciAddr)

	if data, err := os.ReadFile(filepath.Join(devDir, "temperature")); err != nil || string(data) != "45\n" {
		t.Errorf("temperature file = %q (err %v), want \"45\\n\"", string(data), err)
	}
	healthFile := filepath.Join(devDir, "health")
	if data, err := os.ReadFile(healthFile); err != nil || string(data) != types.Healthy+"\n" {
		t.Errorf("health file = %q (err %v), want Healthy", string(data), err)
	}

	if err := d.SetDeviceHealth(devs[0].ID, types.Unhealthy); err != nil {
		t.Fatalf("SetDeviceHealth: %v", err)
	}
	if data, _ := os.ReadFile(healthFile); string(data) != types.Unhealthy+"\n" {
		t.Errorf("health file after SetDeviceHealth = %q, want Unhealthy", string(data))
	}
}
