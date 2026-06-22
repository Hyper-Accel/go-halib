package topology

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PCIBridgeClass denotes the PCI class code for PCI-to-PCI bridges.
const PCIBridgeClass = "0x0604"

func sysfsDevicePath(baseDir, pciAddr string) string {
	if baseDir != "" {
		return filepath.Join(baseDir, "sys", "bus", "pci", "devices", pciAddr)
	}
	return filepath.Join("/sys", "bus", "pci", "devices", pciAddr)
}

// ReadNUMANode reads the NUMA node identifier for a PCI device.
func ReadNUMANode(pciAddr, baseDir string) (int, error) {
	path := filepath.Join(sysfsDevicePath(baseDir, pciAddr), "numa_node")
	data, err := os.ReadFile(path)
	if err != nil {
		return -1, fmt.Errorf("read numa_node: %w", err)
	}
	value := strings.TrimSpace(string(data))
	numa, err := strconv.Atoi(value)
	if err != nil {
		return -1, fmt.Errorf("parse numa_node %q: %w", value, err)
	}
	return numa, nil
}

// ReadDeviceClass returns the PCI class for given device (e.g., 0x0604).
func ReadDeviceClass(pciAddr, baseDir string) (string, error) {
	path := filepath.Join(sysfsDevicePath(baseDir, pciAddr), "class")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read class file: %w", err)
	}
	class := strings.TrimSpace(string(data))
	if len(class) >= 6 {
		// Trim trailing programming interface bits (e.g., 0x060400 -> 0x0604)
		class = class[:6]
	}
	return class, nil
}

// FindDownstreamPort walks up the PCI tree until it finds a parent bridge device
// (Downstream Port). If no bridge is located, the original device address is returned.
func FindDownstreamPort(pciAddr, baseDir string) (string, error) {
	current := pciAddr
	for depth := 0; depth < 16; depth++ {
		class, err := ReadDeviceClass(current, baseDir)
		if err == nil && strings.HasPrefix(class, PCIBridgeClass) {
			return current, nil
		}
		parent, err := getParentDevice(current, baseDir)
		if err != nil {
			return current, nil
		}
		if parent == current {
			return current, nil
		}
		current = parent
	}
	return pciAddr, fmt.Errorf("exceeded max depth while finding downstream port for %s", pciAddr)
}

// FindUpstreamPort returns the Upstream Port (switch parent) for P2P grouping.
//
//	Root Complex
//	└── Upstream Port (0x0604)        ← FindUpstreamPort returns this
//	    ├── Downstream Port 1 (0x0604) ← FindDownstreamPort returns this
//	    │   └── Device A
//	    └── Downstream Port 2 (0x0604)
//	        └── Device B
func FindUpstreamPort(pciAddr, baseDir string) (string, error) {
	downstreamPort, err := FindDownstreamPort(pciAddr, baseDir)
	if err != nil {
		return pciAddr, err
	}
	if downstreamPort == pciAddr {
		return pciAddr, nil
	}
	upstreamPort, err := getParentDevice(downstreamPort, baseDir)
	if err != nil {
		return downstreamPort, nil
	}
	class, err := ReadDeviceClass(upstreamPort, baseDir)
	if err != nil || !strings.HasPrefix(class, PCIBridgeClass) {
		return downstreamPort, nil
	}
	return upstreamPort, nil
}

func getParentDevice(pciAddr, baseDir string) (string, error) {
	devicePath := sysfsDevicePath(baseDir, pciAddr)
	resolved, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		return "", fmt.Errorf("resolve device path: %w", err)
	}
	parentPath := filepath.Dir(resolved)
	parent := filepath.Base(parentPath)
	if !isValidPCIAddress(parent) {
		return "", fmt.Errorf("parent path %s is not a PCI address", parentPath)
	}
	return parent, nil
}

func isValidPCIAddress(addr string) bool {
	parts := strings.Split(addr, ":")
	if len(parts) != 3 {
		return false
	}
	if len(parts[0]) != 4 {
		return false
	}
	fn := strings.Split(parts[2], ".")
	return len(fn) == 2 && len(fn[0]) == 2 && len(fn[1]) == 1
}
