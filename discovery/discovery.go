// Package discovery finds LPU devices via PCI sysfs (or fake mode) and resolves
// the host /dev/ha node. It is k8s-free: consumers compose a Discoverer with
// driver.HWProber for health and add their own kubelet/CDI wrapping.
//
// Ported from bertha-device-plugin/pkg/device, with the k8s manager methods
// (GetContainerAllocate) and the health methods (CheckDeviceHealth) left in the
// device-plugin — only the pure discovery moves here.
package discovery

import (
	"fmt"

	"github.com/Hyper-Accel/go-halib/types"
)

// Mode selects the discovery backend.
type Mode string

const (
	ModeFake Mode = "fake"
	ModePCI  Mode = "pci"
)

// Discoverer enumerates LPU devices.
type Discoverer interface {
	GetDevices() ([]*types.Device, error)
	GetDeviceCount() (int, error)
	GetBaseDir() string // fake base dir; "" for pci
}

// Config configures discovery. Fake-only fields are ignored in pci mode.
type Config struct {
	Mode        Mode
	VendorID    string
	DeviceClass string

	// fake mode
	FakeCount        int
	FakeBaseDir      string
	FakeCreateFiles  bool
	DevicesPerSwitch int
	DeviceID         string
}

// New builds a Discoverer per cfg.Mode (defaults to fake). For pci it scans
// immediately so the returned Discoverer is ready to serve devices.
func New(cfg Config) (Discoverer, error) {
	mode := cfg.Mode
	if mode == "" {
		mode = ModeFake
	}
	vendorID := cfg.VendorID
	if vendorID == "" {
		vendorID = "1eff" // device-plugin default
	}
	deviceClass := cfg.DeviceClass
	if deviceClass == "" {
		deviceClass = "0x120000"
	}

	switch mode {
	case ModePCI:
		d := NewPCIDiscoverer(vendorID, deviceClass)
		if err := d.ScanDevices(); err != nil {
			return nil, fmt.Errorf("scan pci devices: %w", err)
		}
		return d, nil
	case ModeFake:
		return NewFakeDiscoverer(FakeConfig{
			Count:            cfg.FakeCount,
			BaseDir:          cfg.FakeBaseDir,
			VendorID:         vendorID,
			DeviceID:         cfg.DeviceID,
			DeviceClass:      deviceClass,
			CreateFiles:      cfg.FakeCreateFiles,
			DevicesPerSwitch: cfg.DevicesPerSwitch,
			IOMMUGroup:       -1,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported discovery mode %q", mode)
	}
}
