// Package cdi generates a CNCF CDI spec mapping LPU /dev nodes into containers.
// Ported from bertha-device-plugin/pkg/cdi. The CDI kind (vendor/class) is
// PARAMETRIZED — the lib is vendor-neutral, so the consumer injects the kind:
// device-plugin passes "hyperaccel.ai"/"bertha" today (wire-compat), and flips
// to "lpu" in the generic-rename pass. This is the single place that decision lives.
package cdi

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Hyper-Accel/go-halib/types"
	cdiSpecs "tags.cncf.io/container-device-interface/specs-go"
)

// UMD artifact host paths. These are the user-mode driver files the kernel
// driver installs on the host; they are mounted read-only into every LPU
// container (a device node without the UMD lib cannot open the LPU). Matches the
// kernel driver's CDI (ha-cdi.py) so this is a drop-in replacement.
//
// They are hardcoded to the driver's current install layout. If the driver team
// renames an artifact or moves an install path, update the matching constant
// here — it is the single place these paths live.
const (
	UMDLibPath    = "/usr/local/lib/libha_driver.so"           // runtime shared library
	UMDHeaderPath = "/usr/local/include/ha_driver.h"           // build-time header
	UMDPkgCfgPath = "/usr/local/lib/pkgconfig/libha_driver.pc" // pkg-config metadata
)

// umdHostPaths is the ordered set mounted into every LPU container.
var umdHostPaths = []string{UMDLibPath, UMDHeaderPath, UMDPkgCfgPath}

// umdMounts returns ro bind mounts for the UMD artifacts (skipped entirely in
// fake mode, which has no real driver).
//
// The mounts are emitted unconditionally in real mode — no os.Stat existence
// check. The paths are HOST paths, but the check ran against the CALLING
// PROCESS's filesystem, which is a different namespace: the device-plugin image
// happens to bundle a (stale) copy of the UMD so its stat passed, while the
// distroless ha-ctk daemon image has no UMD so its stat failed and it silently
// dropped every mount — producing a spec with the device node but no driver
// library, i.e. a container that sees /dev/haN but cannot open the LPU. Since
// the UMD is a hard requirement installed by the kernel driver (per umdHostPaths'
// own contract), a missing file is a real error surfaced at container start
// ("no such file or directory" on the bind), not something to paper over by
// omitting the mount. This makes the spec identical regardless of which image
// generates it.
func umdMounts(isFake bool) []*cdiSpecs.Mount {
	if isFake {
		return nil
	}
	ms := make([]*cdiSpecs.Mount, 0, len(umdHostPaths))
	for _, p := range umdHostPaths {
		ms = append(ms, &cdiSpecs.Mount{
			HostPath:      p,
			ContainerPath: p,
			Options:       []string{"ro", "nosuid", "nodev", "bind"},
		})
	}
	return ms
}

const (
	DefaultVersion = "0.5.0" // CDI spec version compatible with containerd 1.6+
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
		// Colons become underscores because of the Kubernetes path, not the CDI
		// spec. CDI itself accepts a colon in a device name — parser.ValidateDeviceName
		// allows ':' and a plain `docker run --device hyperaccel.ai/lpu=0000:9c:00.0`
		// works. What rejects it is cdi.AnnotationKey: under Kubernetes the device
		// plugin hands the device ID to kubelet, which passes it to the runtime as
		// the annotation key `cdi.k8s.io/<plugin>_<deviceID>`, and annotation keys
		// permit only alphanumerics, '_', '-' and '.'.
		//
		// The failure mode this prevents is quiet: a colon name works on a plain
		// host and breaks only on Kubernetes, so anyone testing with docker would
		// conclude the sanitizing is unnecessary. See TestDeviceNameSurvivesTheKubernetesPath.
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
