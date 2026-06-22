package driver

// MockProber is a test prober with injectable behavior.
type MockProber struct {
	PingFunc        func(deviceIndex int) error
	TemperatureFunc func(deviceIndex int) (int32, error)
}

func (m MockProber) Ping(deviceIndex int) error {
	if m.PingFunc == nil {
		return nil
	}
	return m.PingFunc(deviceIndex)
}

func (m MockProber) GetTemperature(deviceIndex int) (int32, error) {
	if m.TemperatureFunc == nil {
		return 25, nil
	}
	return m.TemperatureFunc(deviceIndex)
}
