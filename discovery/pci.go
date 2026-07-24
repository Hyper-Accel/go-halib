package discovery

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Hyper-Accel/go-halib/types"
)

// sysfsPCIDevices is the sysfs base for PCI device directories. Tests override this.
var sysfsPCIDevices = "/sys/bus/pci/devices"

// PCIDiscoverer enumerates real PCI devices by walking sysfs.
// Ported from bertha-device-plugin/pkg/device/pci_manager.go (scan part only;
// health probing and the kubelet allocate wrapper stay in the device-plugin).
type PCIDiscoverer struct {
	devices     []*types.Device
	mu          sync.RWMutex
	vendorID    string
	deviceClass string
}

// NewPCIDiscoverer returns a discoverer filtering by PCI vendor id + device class.
func NewPCIDiscoverer(vendorID, deviceClass string) *PCIDiscoverer {
	return &PCIDiscoverer{
		devices:     make([]*types.Device, 0),
		vendorID:    vendorID,
		deviceClass: deviceClass,
	}
}

// ScanDevices walks sysfs directly, reading vendor/device/class attributes.
func (p *PCIDiscoverer) ScanDevices() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	entries, err := os.ReadDir(sysfsPCIDevices)
	if err != nil {
		return fmt.Errorf("read %s: %w", sysfsPCIDevices, err)
	}

	wantVendor := normalizeHexID(p.vendorID)
	wantClass := normalizeHexID(p.deviceClass)

	devices := make([]*types.Device, 0)
	for _, e := range entries {
		pciAddr := e.Name()
		pciDir := filepath.Join(sysfsPCIDevices, pciAddr)

		vendor, err := readSysAttr(filepath.Join(pciDir, "vendor"))
		if err != nil {
			continue
		}
		if wantVendor != "" && normalizeHexID(vendor) != wantVendor {
			continue
		}

		class, err := readSysAttr(filepath.Join(pciDir, "class"))
		if err != nil {
			continue
		}
		if wantClass != "" && normalizeHexID(class) != wantClass {
			continue
		}

		deviceID, _ := readSysAttr(filepath.Join(pciDir, "device"))
		dev := &types.Device{
			ID:       fmt.Sprintf("%s-%s", normalizeHexID(vendor), pciAddr),
			Health:   types.Healthy,
			PCIAddr:  pciAddr,
			VendorID: normalizeHexID(vendor),
			DeviceID: normalizeHexID(deviceID),
			NUMANode: -1,
		}
		if name, devDir, err := resolveCharDevName(pciAddr); err != nil {
			log.Printf("[go-halib] could not resolve /dev node for %s: %v; CDI will fall back to PCI-bus derived index", pciAddr, err)
		} else {
			dev.DevName = name
			// Stable board serial from the class device dir (survives reseat).
			// Best-effort: older drivers may not expose it.
			if serial, e := readSysAttr(filepath.Join(devDir, "serial")); e == nil {
				dev.Serial = serial
			}
			log.Printf("[go-halib] PCI %s -> /dev/%s (serial=%q)", pciAddr, name, dev.Serial)
		}
		devices = append(devices, dev)
	}

	p.devices = devices
	log.Printf("[go-halib] scanned %d PCI devices (vendor=%s class=%s)", len(devices), p.vendorID, p.deviceClass)
	return nil
}

func readSysAttr(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func normalizeHexID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.TrimPrefix(s, "0x")
}

// resolveCharDevName returns the char device name (e.g. "ha0") AND the sysfs
// directory of that class device (e.g. "<pci>/ha/ha0"), which is also where the
// driver exposes per-device attributes like serial. devDir lets callers read
// those without re-walking. Both are "" on error.
func resolveCharDevName(pciAddr string) (name, devDir string, err error) {
	pciDir := filepath.Join(sysfsPCIDevices, pciAddr)
	entries, e := os.ReadDir(pciDir)
	if e != nil {
		return "", "", fmt.Errorf("read %s: %w", pciDir, e)
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		classDir := filepath.Join(pciDir, ent.Name())
		subentries, e := os.ReadDir(classDir)
		if e != nil {
			continue
		}
		for _, se := range subentries {
			if !se.IsDir() {
				continue
			}
			dir := filepath.Join(classDir, se.Name())
			if _, e := os.Stat(filepath.Join(dir, "dev")); e == nil {
				return se.Name(), dir, nil
			}
		}
	}
	return "", "", fmt.Errorf("no class device subdirectory with a dev file under %s", pciDir)
}

// GetDevices returns a copy of the discovered device slice (shared *Device
// pointers, so a consumer's health loop can update Health in place).
func (p *PCIDiscoverer) GetDevices() ([]*types.Device, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	devices := make([]*types.Device, len(p.devices))
	copy(devices, p.devices)
	return devices, nil
}

// GetDeviceCount returns the number of discovered PCI devices.
func (p *PCIDiscoverer) GetDeviceCount() (int, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.devices), nil
}

// GetBaseDir returns "" for real PCI devices.
func (p *PCIDiscoverer) GetBaseDir() string { return "" }
