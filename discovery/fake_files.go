package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Hyper-Accel/go-halib/types"
)

// createSwitchStructure creates fake PCIe switch (bridge) sysfs directories.
func (f *FakeDiscoverer) createSwitchStructure() {
	devicesPerSwitch := f.config.DevicesPerSwitch
	if devicesPerSwitch <= 0 {
		devicesPerSwitch = 4
	}
	numSwitches := (f.config.Count + devicesPerSwitch - 1) / devicesPerSwitch
	for i := 0; i < numSwitches; i++ {
		switchAddr := f.getSwitchAddr(i)
		switchPath := filepath.Join(f.config.BaseDir, "sys", "bus", "pci", "devices", switchAddr)
		os.MkdirAll(switchPath, 0o755)
		os.WriteFile(filepath.Join(switchPath, "class"), []byte("0x060400\n"), 0o644) // PCIe bridge
		os.WriteFile(filepath.Join(switchPath, "vendor"), []byte("0x10b5\n"), 0o644)   // PLX/Broadcom
		os.WriteFile(filepath.Join(switchPath, "device"), []byte("0x8724\n"), 0o644)
		os.WriteFile(filepath.Join(switchPath, "numa_node"), []byte(fmt.Sprintf("%d\n", i)), 0o644)
	}
}

// createDeviceSymlink links a device dir to a nested path under its switch (for
// parent resolution during topology walks).
func (f *FakeDiscoverer) createDeviceSymlink(deviceAddr, switchAddr string) {
	devicePath := filepath.Join(f.config.BaseDir, "sys", "bus", "pci", "devices", deviceAddr)
	switchPath := filepath.Join(f.config.BaseDir, "sys", "bus", "pci", "devices", switchAddr)
	nestedDevicePath := filepath.Join(switchPath, deviceAddr)
	os.MkdirAll(nestedDevicePath, 0o755)
	os.RemoveAll(devicePath)
	os.Symlink(nestedDevicePath, devicePath)
}

// createDeviceFiles writes the fake sysfs files + /dev node for one device.
func (f *FakeDiscoverer) createDeviceFiles(pciAddr, vendorID, deviceID string, index, numaNode int, bridgeID string) {
	sysfsPath := filepath.Join(f.config.BaseDir, "sys", "bus", "pci", "devices", pciAddr)
	os.MkdirAll(sysfsPath, 0o755)

	baseClass, subClass, progIface := f.parseDeviceClass()
	f.createSysfsBasicFiles(sysfsPath, vendorID, deviceID)
	f.createSysfsModaliasFile(sysfsPath, vendorID, deviceID, baseClass, subClass, progIface)
	f.createSysfsTopologyFiles(sysfsPath, pciAddr, index, numaNode, bridgeID)
	f.createHealthFile(sysfsPath)
	f.createTemperatureFile(sysfsPath)
	f.createDevFiles(pciAddr, index)
}

// parseDeviceClass splits "0xBBSSPP" into base, sub, programming-interface.
func (f *FakeDiscoverer) parseDeviceClass() (baseClass, subClass, progIface string) {
	if len(f.config.DeviceClass) >= 8 && f.config.DeviceClass[:2] == "0x" {
		baseClass = f.config.DeviceClass[2:4]
		subClass = f.config.DeviceClass[4:6]
		progIface = f.config.DeviceClass[6:8]
		return baseClass, subClass, progIface
	}
	return "12", "00", "00"
}

func (f *FakeDiscoverer) createSysfsBasicFiles(sysfsPath, vendorID, deviceID string) {
	w := func(name, content string) { os.WriteFile(filepath.Join(sysfsPath, name), []byte(content), 0o644) }
	w("vendor", fmt.Sprintf("0x%s\n", vendorID))
	w("device", fmt.Sprintf("0x%s\n", deviceID))
	w("revision", fmt.Sprintf("0x%s\n", f.config.RevisionID))
	w("subsystem_vendor", fmt.Sprintf("0x%s\n", f.config.SubsystemVendorID))
	w("subsystem_device", fmt.Sprintf("0x%s\n", f.config.SubsystemDeviceID))
	w("class", fmt.Sprintf("%s\n", f.config.DeviceClass))
}

func (f *FakeDiscoverer) createSysfsModaliasFile(sysfsPath, vendorID, deviceID, baseClass, subClass, progIface string) {
	pad := func(s string) string {
		s = strings.ToLower(strings.TrimPrefix(s, "0x"))
		return fmt.Sprintf("%08s", s)
	}
	modalias := fmt.Sprintf("pci:v%sd%ssv%ssd%sbc%ssc%si%s\n",
		pad(vendorID), pad(deviceID), pad(f.config.SubsystemVendorID), pad(f.config.SubsystemDeviceID),
		baseClass, subClass, progIface)
	os.WriteFile(filepath.Join(sysfsPath, "modalias"), []byte(modalias), 0o644)
}

func (f *FakeDiscoverer) createSysfsTopologyFiles(sysfsPath, pciAddr string, index, numaNode int, bridgeID string) {
	os.WriteFile(filepath.Join(sysfsPath, "numa_node"), []byte(fmt.Sprintf("%d\n", numaNode)), 0o644)
	if bridgeID != "" {
		os.WriteFile(filepath.Join(sysfsPath, "bridge_id"), []byte(fmt.Sprintf("%s\n", bridgeID)), 0o644)
	}
	iommuGroupNum := f.config.IOMMUGroup
	if iommuGroupNum < 0 {
		iommuGroupNum = index
	}
	iommuGroupsDir := filepath.Join(f.config.BaseDir, "sys", "kernel", "iommu_groups", fmt.Sprintf("%d", iommuGroupNum), "devices")
	os.MkdirAll(iommuGroupsDir, 0o755)
	os.Symlink(filepath.Join("../../../../kernel/iommu_groups", fmt.Sprintf("%d", iommuGroupNum)), filepath.Join(sysfsPath, "iommu_group"))
	os.Symlink(filepath.Join("../../../../bus/pci/devices", pciAddr), filepath.Join(iommuGroupsDir, pciAddr))
}

// createDevFiles creates /dev/ha{index} (char node via mknod; regular-file
// fallback if mknod fails, e.g. non-root or darwin) + a pci-addr symlink.
func (f *FakeDiscoverer) createDevFiles(pciAddr string, index int) {
	devDir := filepath.Join(f.config.BaseDir, "dev")
	os.MkdirAll(devDir, 0o755)

	cdiIndex := extractBusIndex(pciAddr)
	devFile := filepath.Join(devDir, fmt.Sprintf("ha%d", cdiIndex))
	os.Remove(devFile)

	// Major 1 (mem), minor 100+index to avoid colliding with system devices.
	dev := int((1 << 8) | (100 + index))
	if err := syscall.Mknod(devFile, syscall.S_IFCHR|0o666, dev); err != nil {
		os.WriteFile(devFile, []byte("fake device"), 0o644)
		os.Chmod(devFile, 0o666)
	}
	os.Symlink(filepath.Base(devFile), filepath.Join(devDir, fmt.Sprintf("pci-%s", pciAddr)))
}

// extractBusIndex derives the fake /dev index: DDDD:BB:SS.F -> bus(hex) - 1.
// Matches cdi.ExtractDeviceIndex so the fake /dev node lines up with the CDI hostPath.
func extractBusIndex(pciAddr string) int {
	parts := strings.Split(pciAddr, ":")
	if len(parts) < 2 {
		return 0
	}
	busPart := strings.Split(parts[1], ".")[0]
	bus, err := strconv.ParseInt(busPart, 16, 64)
	if err != nil {
		return 0
	}
	idx := int(bus) - 1
	if idx < 0 {
		idx = 0
	}
	return idx
}

// createHealthFile writes the initial health fixture ("Healthy"). The consumer's
// health loop reads this until bertha-smi/real telemetry replaces it.
func (f *FakeDiscoverer) createHealthFile(sysfsPath string) {
	os.WriteFile(filepath.Join(sysfsPath, "health"), []byte(types.Healthy+"\n"), 0o644)
}

func (f *FakeDiscoverer) createTemperatureFile(sysfsPath string) {
	os.WriteFile(filepath.Join(sysfsPath, "temperature"), []byte("45\n"), 0o644)
}

func (f *FakeDiscoverer) updateHealthFile(pciAddr, health string) {
	healthFile := filepath.Join(f.config.BaseDir, "sys", "bus", "pci", "devices", pciAddr, "health")
	os.WriteFile(healthFile, []byte(health+"\n"), 0o644)
}

// Cleanup removes all fake device files.
func (f *FakeDiscoverer) Cleanup() error {
	if f.config.CreateFiles {
		return os.RemoveAll(f.config.BaseDir)
	}
	return nil
}
