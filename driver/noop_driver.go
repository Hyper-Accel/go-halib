//go:build !linux || !cgo || !hyperdriver

package driver

// HADriver is a compile-safe fallback for builds without hyperdriver linkage.
type HADriver struct{}

func (HADriver) Ping(deviceIndex int) error {
	return nil
}

func (HADriver) GetTemperature(deviceIndex int) (int32, error) {
	return 25, nil
}
