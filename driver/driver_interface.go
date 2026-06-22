package driver

// HWProber abstracts hardware health probing.
//
// Backends are build-tag selected: ha_driver.go (linux && cgo && hyperdriver)
// links libha_driver.so for real probing; noop_driver.go is the fallback for
// every other build (CI, ha-ctk distroless CGO_ENABLED=0). MockProber is for tests.
//
// The ultimate target is an ha_smi-backed prober (SOFT-2098); because consumers
// depend only on this interface, that is a backend addition, not a rewrite.
type HWProber interface {
	Ping(deviceIndex int) error
	GetTemperature(deviceIndex int) (int32, error)
}
