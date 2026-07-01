// Package cdi generates a CNCF CDI spec mapping LPU /dev nodes into containers.
// Ported from bertha-device-plugin/pkg/cdi. The CDI kind (vendor/class) is
// PARAMETRIZED — the lib is vendor-neutral, so the consumer injects the kind:
// device-plugin passes "hyperaccel.ai"/"bertha" today (wire-compat), and flips
// to "lpu" in the generic-rename pass. This is the single place that decision lives.
package cdi

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Hyper-Accel/go-halib/types"
	cdiSpecs "tags.cncf.io/container-device-interface/specs-go"
)

// umdHostPaths are the user-mode driver artifacts the kernel driver installs on
// the host. They must be mounted into the container alongside the device node,
// otherwise the device node is present but no app can open the LPU (no UMD lib).
// Matches the kernel driver's CDI (ha-cdi.py) so this is a drop-in replacement.
var umdHostPaths = []string{
	"/usr/local/lib/libha_driver.so",
	"/usr/local/include/ha_driver.h",
	"/usr/local/lib/pkgconfig/libha_driver.pc",
}

// umdMounts returns ro bind mounts for the UMD artifacts that actually exist on
// the host (skipped entirely in fake mode, which has no real driver).
func umdMounts(isFake bool) []*cdiSpecs.Mount {
	if isFake {
		return nil
	}
	var ms []*cdiSpecs.Mount
	for _, p := range umdHostPaths {
		if _, err := os.Stat(p); err == nil {
			ms = append(ms, &cdiSpecs.Mount{
				HostPath:      p,
				ContainerPath: p,
				Options:       []string{"ro", "nosuid", "nodev", "bind"},
			})
		}
	}
	return ms
}

const (
	DefaultVersion = "0.5.0"        // CDI spec version compatible with containerd 1.6+
	DefaultVendor  = "hyperaccel.ai"
	// DefaultClass is the generic LPU class. device-plugin currently passes
	// "bertha" for wire compatibility; flip callers to "lpu" in the rename pass.
	DefaultClass = "lpu"

	// DefaultFakeBaseDir roots fake-mode device host paths when GenerateSpec is
	// called with an empty fakeBaseDir. It must match where the fake discoverer
	// creates the device nodes.
	DefaultFakeBaseDir = "/tmp/fake-devices"
)

// UpdateSpec builds and writes the CDI spec for devices under "<vendor>/<class>".
// With no devices it removes the spec file instead.
func UpdateSpec(devices []*types.Device, vendor, class string, isFake bool, specDir, specFile string) error {
	g := NewGenerator(specDir, specFile)
	if len(devices) == 0 {
		return g.Remove()
	}
	spec, err := GenerateSpec(devices, vendor, class, isFake, "")
	if err != nil {
		return fmt.Errorf("generate CDI spec: %w", err)
	}
	if err := g.Write(spec); err != nil {
		return fmt.Errorf("write CDI spec: %w", err)
	}
	return nil
}

// GenerateSpec builds a CDI spec for devices under kind "<vendor>/<class>". In
// fake mode device host paths are rooted at fakeBaseDir (DefaultFakeBaseDir when
// empty), matching where the fake discoverer creates the device nodes.
func GenerateSpec(devices []*types.Device, vendor, class string, isFake bool, fakeBaseDir string) (*cdiSpecs.Spec, error) {
	if len(devices) == 0 {
		return nil, fmt.Errorf("no devices provided")
	}
	if fakeBaseDir == "" {
		fakeBaseDir = DefaultFakeBaseDir
	}
	var deviceSpecs []cdiSpecs.Device
	var allNodes []*cdiSpecs.DeviceNode
	seen := map[string]bool{}
	add := func(name string, nodes []*cdiSpecs.DeviceNode) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		deviceSpecs = append(deviceSpecs, cdiSpecs.Device{
			Name:           name,
			ContainerEdits: cdiSpecs.ContainerEdits{DeviceNodes: nodes},
		})
	}
	for _, dev := range devices {
		ds, err := createDeviceSpec(dev, isFake, fakeBaseDir)
		if err != nil {
			return nil, fmt.Errorf("create device spec for %s: %w", dev.ID, err)
		}
		node := ds.ContainerEdits.DeviceNodes[0]
		allNodes = append(allNodes, node)
		// Two aliases per device, both mapping to the same /dev node:
		//   - a human-friendly index ("0"), derived from the /dev/haN node name,
		//     so `--device <kind>=0` works like nvidia.com/gpu=0;
		//   - the stable PCI-BDF ("0000_01_00.0"), for scripted/reproducible
		//     pinning and backward-compat with the device-plugin's references.
		idxName := strings.TrimPrefix(strings.TrimPrefix(node.Path, "/dev/"), "ha")
		add(idxName, []*cdiSpecs.DeviceNode{node})
		add(ds.Name, []*cdiSpecs.DeviceNode{node}) // ds.Name is the BDF alias
	}
	// "all" injects every device at once (mirrors nvidia.com/gpu=all): for a
	// single container that uses every LPU (e.g. tensor/pipeline parallel), NOT
	// for sharing one device across containers.
	add("all", allNodes)

	return &cdiSpecs.Spec{
		Version: DefaultVersion,
		Kind:    fmt.Sprintf("%s/%s", vendor, class),
		Devices: deviceSpecs,
		ContainerEdits: cdiSpecs.ContainerEdits{
			Mounts: umdMounts(isFake),
		},
	}, nil
}

func createDeviceSpec(dev *types.Device, isFake bool, fakeBaseDir string) (*cdiSpecs.Device, error) {
	// Real-mode prefers the name the kernel driver actually registered
	// (e.g. "ha0", from sysfs) because drivers allocate /dev/haN in probe
	// order, not by PCI bus number — the legacy "bus - 1" derivation collides
	// on multi-LPU hosts. Fake-mode (and any real device with no resolved
	// name) falls back to the bus-derived index so fixtures stay byte-identical.
	var nodeName string
	if !isFake && dev.DevName != "" {
		nodeName = dev.DevName
	} else {
		nodeName = fmt.Sprintf("ha%d", ExtractDeviceIndex(dev.PCIAddr))
	}

	containerPath := "/dev/" + nodeName
	hostPath := containerPath
	if isFake {
		hostPath = strings.TrimRight(fakeBaseDir, "/") + "/dev/" + nodeName
	}

	deviceNode := &cdiSpecs.DeviceNode{
		Path:        containerPath,
		HostPath:    hostPath,
		Permissions: "rw",
	}

	return &cdiSpecs.Device{
		// Sanitize colons from the PCI address for the CDI device name (regex requirement).
		Name: strings.ReplaceAll(dev.PCIAddr, ":", "_"),
		ContainerEdits: cdiSpecs.ContainerEdits{
			DeviceNodes: []*cdiSpecs.DeviceNode{deviceNode},
		},
	}, nil
}

// ExtractDeviceIndex derives the legacy fallback index: 0000:01:00.0 -> bus 0x01 -> 0.
func ExtractDeviceIndex(pciAddr string) int {
	parts := strings.Split(pciAddr, ":")
	if len(parts) < 2 {
		return 0
	}
	busPart := strings.Split(parts[1], ".")[0]
	bus, err := strconv.ParseInt(busPart, 16, 64)
	if err != nil {
		return 0
	}
	index := int(bus) - 1
	if index < 0 {
		index = 0
	}
	return index
}
