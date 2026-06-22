// Package topology builds the NUMA + PCIe-switch topology of LPU devices from
// sysfs. Ported from bertha-device-plugin/pkg/topology (glog -> stdlib log).
// It is decoupled from types.Device: callers pass DeviceInfo (id + pci addr).
package topology

import (
	"fmt"
	"log"
	"sync"
)

// Topology represents the NUMA and PCIe topology of devices.
type Topology struct {
	NUMA map[int]*NUMANode
	mu   sync.RWMutex
}

// NUMANode represents a NUMA node with its PCIe ports.
type NUMANode struct {
	ID    int
	Ports map[string]*PCIePort
}

// PCIePort represents a PCIe Downstream Port with its devices.
type PCIePort struct {
	DownstreamPort string   // PCI address of the Downstream Port (direct parent of device)
	UpstreamPort   string   // PCI address of the Upstream Port (Switch's parent port, for P2P grouping)
	Devices        []string // Device IDs (format: "vendor-pciaddr")
}

// NewTopology creates a new empty topology.
// Structure: NUMA Node -> PCIe Port (Downstream Port) -> Devices
func NewTopology() *Topology {
	return &Topology{NUMA: make(map[int]*NUMANode)}
}

// RecordDevice registers a device in the topology graph.
func (t *Topology) RecordDevice(deviceID string, numaNode int, downstreamPort string, upstreamPort string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	numa, exists := t.NUMA[numaNode]
	if !exists {
		numa = &NUMANode{ID: numaNode, Ports: make(map[string]*PCIePort)}
		t.NUMA[numaNode] = numa
	}

	port, exists := numa.Ports[downstreamPort]
	if !exists {
		port = &PCIePort{DownstreamPort: downstreamPort, UpstreamPort: upstreamPort, Devices: make([]string, 0)}
		numa.Ports[downstreamPort] = port
	}

	port.Devices = append(port.Devices, deviceID)
}

// GetDevicesByNUMA returns all devices in a specific NUMA node.
func (t *Topology) GetDevicesByNUMA(numaNode int) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	numa, exists := t.NUMA[numaNode]
	if !exists {
		return []string{}
	}
	devices := make([]string, 0)
	for _, port := range numa.Ports {
		devices = append(devices, port.Devices...)
	}
	return devices
}

// GetDevicesByPort returns all devices under a specific port in a NUMA node.
func (t *Topology) GetDevicesByPort(numaNode int, portID string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	numa, exists := t.NUMA[numaNode]
	if !exists {
		return []string{}
	}
	port, exists := numa.Ports[portID]
	if !exists {
		return []string{}
	}
	devices := make([]string, len(port.Devices))
	copy(devices, port.Devices)
	return devices
}

// GetAllNUMANodes returns all NUMA node IDs in the topology.
func (t *Topology) GetAllNUMANodes() []int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	nodes := make([]int, 0, len(t.NUMA))
	for numaID := range t.NUMA {
		nodes = append(nodes, numaID)
	}
	return nodes
}

// GetPortsInNUMA returns all port IDs in a specific NUMA node.
func (t *Topology) GetPortsInNUMA(numaNode int) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	numa, exists := t.NUMA[numaNode]
	if !exists {
		return []string{}
	}
	ports := make([]string, 0, len(numa.Ports))
	for portID := range numa.Ports {
		ports = append(ports, portID)
	}
	return ports
}

// GetDeviceLocation returns the complete location info for a device.
func (t *Topology) GetDeviceLocation(deviceID string) (numaNode int, downstreamPort string, upstreamPort string, found bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for numaID, numa := range t.NUMA {
		for pid, port := range numa.Ports {
			for _, devID := range port.Devices {
				if devID == deviceID {
					return numaID, pid, port.UpstreamPort, true
				}
			}
		}
	}
	return -1, "", "", false
}

// GetAllDevicesByLocation returns all devices grouped by location: numaNode -> portID -> []deviceID.
func (t *Topology) GetAllDevicesByLocation() map[int]map[string][]string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[int]map[string][]string)
	for numaID, numa := range t.NUMA {
		result[numaID] = make(map[string][]string)
		for portID, port := range numa.Ports {
			devices := make([]string, len(port.Devices))
			copy(devices, port.Devices)
			result[numaID][portID] = devices
		}
	}
	return result
}

// String returns a string representation of the topology for debugging.
func (t *Topology) String() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result string
	for numaID, numa := range t.NUMA {
		result += fmt.Sprintf("NUMA %d:\n", numaID)
		for portID, port := range numa.Ports {
			result += fmt.Sprintf("  Port %s (UpstreamPort: %s): %d devices\n", portID, port.UpstreamPort, len(port.Devices))
			for _, deviceID := range port.Devices {
				result += fmt.Sprintf("    - %s\n", deviceID)
			}
		}
	}
	return result
}

// DeviceInfo contains the information needed to build topology.
type DeviceInfo struct {
	ID      string // Device ID (format: "vendor-pciaddr")
	PCIAddr string // PCI address (format: "0000:01:00.0")
}

// Build constructs a topology map from a list of devices.
// baseDir is empty for real devices, or "/tmp/fake-devices" for fake devices.
func Build(devices []DeviceInfo, baseDir string) (*Topology, error) {
	topology := NewTopology()

	for _, dev := range devices {
		numaNode, err := ReadNUMANode(dev.PCIAddr, baseDir)
		if err != nil {
			log.Printf("[go-halib] failed to read NUMA node for %s: %v (using -1)", dev.PCIAddr, err)
			numaNode = -1
		}

		portID, err := FindDownstreamPort(dev.PCIAddr, baseDir)
		if err != nil {
			log.Printf("[go-halib] failed to find downstream port for %s: %v (using device addr)", dev.PCIAddr, err)
			portID = dev.PCIAddr
		}

		switchID, err := FindUpstreamPort(dev.PCIAddr, baseDir)
		if err != nil {
			log.Printf("[go-halib] failed to find upstream port for %s: %v (using port)", dev.PCIAddr, err)
			switchID = portID
		}

		topology.RecordDevice(dev.ID, numaNode, portID, switchID)
	}

	return topology, nil
}
