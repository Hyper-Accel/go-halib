// Package types holds the vendor-neutral device vocabulary shared across the
// go-halib packages and its consumers (device-plugin, ha-ctk, BFD).
package types

// Health values for Device.Health. Kept as plain strings (k8s-free); the
// device-plugin maps these to kubelet pluginapi constants, which have the
// same underlying values ("Healthy"/"Unhealthy").
const (
	Healthy   = "Healthy"
	Unhealthy = "Unhealthy"
)

// Device is a single LPU device.
type Device struct {
	ID       string // logical id, e.g. "<vendorID>-<pciAddr>"
	DevName  string // host /dev node, e.g. "ha0" (empty in fake mode)
	Serial   string // stable board serial from /sys/class/ha/<dev>/serial; survives reseat/slot moves (empty if the driver doesn't expose it)
	PCIAddr  string
	VendorID string
	DeviceID string
	NUMANode int    // -1 if unknown; filled by topology, not the scan
	BridgeID string // PCIe switch/bridge id; filled by topology
	Health   string // Healthy | Unhealthy
}

// Stat is a raw telemetry sample for one device. It grows toward ha_smi's
// HaDeviceInfo (power/dram/cores) as SOFT-2098 lands; backends fill what they
// can and leave the rest zero.
type Stat struct {
	Ping        bool
	Temperature int32 // Celsius
	// Power, DRAMTotal, DRAMFree, CoreCount — added when ha_smi exposes them.
}
