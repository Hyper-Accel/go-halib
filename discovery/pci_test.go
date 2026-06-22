package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCharDevName_LeafDir(t *testing.T) {
	tmp := t.TempDir()
	sysfsPCIDevices = filepath.Join(tmp, "sys/bus/pci/devices")
	t.Cleanup(func() { sysfsPCIDevices = "/sys/bus/pci/devices" })

	pciAddr := "0000:19:00.0"
	leaf := filepath.Join(sysfsPCIDevices, pciAddr, "ha", "ha0")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leaf, "dev"), []byte("511:0\n"), 0o644); err != nil {
		t.Fatalf("setup write dev: %v", err)
	}

	got, err := resolveCharDevName(pciAddr)
	if err != nil {
		t.Fatalf("resolveCharDevName: %v", err)
	}
	if got != "ha0" {
		t.Errorf("got %q, want %q", got, "ha0")
	}
}

func TestResolveCharDevName_MissingClass(t *testing.T) {
	tmp := t.TempDir()
	sysfsPCIDevices = filepath.Join(tmp, "sys/bus/pci/devices")
	t.Cleanup(func() { sysfsPCIDevices = "/sys/bus/pci/devices" })

	if err := os.MkdirAll(filepath.Join(sysfsPCIDevices, "0000:99:00.0"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := resolveCharDevName("0000:99:00.0"); err == nil {
		t.Error("expected error when no class subdirectory has a dev file, got nil")
	}
}

func TestScanDevices_SysfsDirect(t *testing.T) {
	tmp := t.TempDir()
	sysfsPCIDevices = filepath.Join(tmp, "sys/bus/pci/devices")
	t.Cleanup(func() { sysfsPCIDevices = "/sys/bus/pci/devices" })

	mustWrite := func(p, content string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	for _, fixture := range []struct {
		addr, vendor, class, device, dev string
	}{
		{"0000:19:00.0", "0x20fb", "0x120000", "0x0010", "ha0"},
		{"0000:af:00.0", "0x20fb", "0x120000", "0x0010", "ha1"},
		{"0000:99:00.0", "0x8086", "0x020000", "0x1234", ""},
	} {
		base := filepath.Join(sysfsPCIDevices, fixture.addr)
		mustWrite(filepath.Join(base, "vendor"), fixture.vendor+"\n")
		mustWrite(filepath.Join(base, "device"), fixture.device+"\n")
		mustWrite(filepath.Join(base, "class"), fixture.class+"\n")
		if fixture.dev != "" {
			leaf := filepath.Join(base, "ha", fixture.dev)
			mustWrite(filepath.Join(leaf, "dev"), "511:0\n")
		}
	}

	d := NewPCIDiscoverer("20fb", "0x120000")
	if err := d.ScanDevices(); err != nil {
		t.Fatalf("ScanDevices: %v", err)
	}
	devs, _ := d.GetDevices()
	if len(devs) != 2 {
		t.Fatalf("expected 2 matched devices, got %d", len(devs))
	}

	got := map[string]string{}
	for _, dv := range devs {
		got[dv.PCIAddr] = dv.DevName
		if dv.VendorID != "20fb" {
			t.Errorf("VendorID = %s, want 20fb (normalized)", dv.VendorID)
		}
		if dv.DeviceID != "0010" {
			t.Errorf("DeviceID = %s, want 0010 (normalized)", dv.DeviceID)
		}
		// The scan leaves NUMA to topology.Build; it must not assert a node here.
		if dv.NUMANode != -1 {
			t.Errorf("NUMANode = %d, want -1 (scan leaves NUMA to topology)", dv.NUMANode)
		}
	}
	if got["0000:19:00.0"] != "ha0" {
		t.Errorf("DevName for 0000:19:00.0 = %q, want ha0", got["0000:19:00.0"])
	}
	if got["0000:af:00.0"] != "ha1" {
		t.Errorf("DevName for 0000:af:00.0 = %q, want ha1", got["0000:af:00.0"])
	}
}

func TestNormalizeHexID(t *testing.T) {
	cases := map[string]string{
		"0x20fb":   "20fb",
		"0X20FB":   "20fb",
		"20fb":     "20fb",
		" 20FB\n":  "20fb",
		"":         "",
		"0x120000": "120000",
	}
	for in, want := range cases {
		if got := normalizeHexID(in); got != want {
			t.Errorf("normalizeHexID(%q) = %q, want %q", in, got, want)
		}
	}
}
