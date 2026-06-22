package discovery

import (
	"fmt"
	"sync"

	"github.com/Hyper-Accel/go-halib/types"
)

// FakeConfig configures fake device synthesis.
// Ported from bertha-device-plugin/pkg/device FakeDeviceConfig; the ghw-scan
// path (UseGHW) is dropped — go-halib's fake is in-memory only, dep-free.
type FakeConfig struct {
	Count             int
	BaseDir           string // default /tmp/fake-devices
	VendorID          string // default "1f26" (Zebu)
	DeviceID          string // default "0000"
	RevisionID        string // default "01"
	SubsystemVendorID string // default = VendorID
	SubsystemDeviceID string // default "1871"
	DeviceClass       string // default "0x120000"
	NUMANode          int
	NUMANodes         []int    // optional per-device NUMA overrides
	BridgeIDs         []string // optional per-device bridge overrides
	IOMMUGroup        int      // <0 => use device index
	CreateFiles       bool     // create fake sysfs + /dev entries
	DevicesPerSwitch  int      // default 4
}

// FakeDiscoverer synthesizes fake LPU devices (and optionally a fake sysfs +
// /dev tree under BaseDir) for dev/CI without hardware.
type FakeDiscoverer struct {
	devices []*types.Device
	mu      sync.RWMutex
	config  FakeConfig
}

// NewFakeDiscoverer builds the fake devices (and fixtures if CreateFiles).
func NewFakeDiscoverer(config FakeConfig) *FakeDiscoverer {
	if config.Count == 0 {
		config.Count = 1
	}
	if config.BaseDir == "" {
		config.BaseDir = "/tmp/fake-devices"
	}
	if config.VendorID == "" {
		config.VendorID = "1f26"
	}
	if config.DeviceID == "" {
		config.DeviceID = "0000"
	}
	if config.RevisionID == "" {
		config.RevisionID = "01"
	}
	if config.SubsystemVendorID == "" {
		config.SubsystemVendorID = config.VendorID
	}
	if config.SubsystemDeviceID == "" {
		config.SubsystemDeviceID = "1871"
	}
	if config.DeviceClass == "" {
		config.DeviceClass = "0x120000"
	}

	f := &FakeDiscoverer{
		devices: make([]*types.Device, 0, config.Count),
		config:  config,
	}

	if config.CreateFiles {
		f.createSwitchStructure()
	}
	for i := 0; i < config.Count; i++ {
		f.devices = append(f.devices, f.createFakeDevice(i))
	}
	return f
}

func (f *FakeDiscoverer) createFakeDevice(index int) *types.Device {
	devicesPerSwitch := f.config.DevicesPerSwitch
	if devicesPerSwitch <= 0 {
		devicesPerSwitch = 4
	}
	switchGroup := index / devicesPerSwitch
	deviceInGroup := index % devicesPerSwitch

	baseBus := switchBusBase(switchGroup)
	bus := baseBus + 1 + deviceInGroup // +1 skips the switch's own address
	pciAddr := fmt.Sprintf("0000:%02x:%02x.%x", bus, 0x00, 0x0)

	switchAddr := f.getSwitchAddr(switchGroup)

	numaNode := switchGroup
	if len(f.config.NUMANodes) > index {
		numaNode = f.config.NUMANodes[index]
	}
	bridgeID := switchAddr
	if len(f.config.BridgeIDs) > index {
		bridgeID = f.config.BridgeIDs[index]
	}

	device := &types.Device{
		ID:       fmt.Sprintf("%s-%s", f.config.VendorID, pciAddr),
		Health:   types.Healthy,
		PCIAddr:  pciAddr,
		VendorID: f.config.VendorID,
		DeviceID: f.config.DeviceID,
		NUMANode: numaNode,
		BridgeID: bridgeID,
	}

	if f.config.CreateFiles {
		f.createDeviceSymlink(pciAddr, switchAddr)
		f.createDeviceFiles(pciAddr, f.config.VendorID, f.config.DeviceID, index, numaNode, bridgeID)
	}
	return device
}

// switchBusBase returns the base bus number for a switch group:
// switch 0 -> 0x80, 1 -> 0x90, 2 -> 0xa0, ...
func switchBusBase(switchGroup int) int { return 0x80 + (switchGroup * 0x10) }

func (f *FakeDiscoverer) getSwitchAddr(switchGroup int) string {
	return fmt.Sprintf("0000:%02x:00.0", switchBusBase(switchGroup))
}

// GetDevices returns a copy of the synthesized device slice.
func (f *FakeDiscoverer) GetDevices() ([]*types.Device, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	devices := make([]*types.Device, len(f.devices))
	copy(devices, f.devices)
	return devices, nil
}

// GetDeviceCount returns the number of fake devices.
func (f *FakeDiscoverer) GetDeviceCount() (int, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.devices), nil
}

// GetBaseDir returns the fake base directory.
func (f *FakeDiscoverer) GetBaseDir() string { return f.config.BaseDir }

// SetDeviceHealth sets a fake device's health (test helper); updates the fixture
// health file too if CreateFiles. Health reading itself lives in the consumer.
func (f *FakeDiscoverer) SetDeviceHealth(deviceID, health string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, device := range f.devices {
		if device.ID == deviceID {
			device.Health = health
			if f.config.CreateFiles {
				f.updateHealthFile(device.PCIAddr, health)
			}
			return nil
		}
	}
	return fmt.Errorf("device %s not found", deviceID)
}
