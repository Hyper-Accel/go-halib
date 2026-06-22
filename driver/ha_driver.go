//go:build linux && cgo && hyperdriver

package driver

/*
#cgo LDFLAGS: -lha_driver
#include <stdint.h>
#include <stdlib.h>
#include "ha_driver.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// HADriver probes real Bertha hardware through hyperdriver UMD.
type HADriver struct{}

func (HADriver) Ping(deviceIndex int) error {
	handle, err := openDevice(deviceIndex)
	if err != nil {
		return err
	}
	defer closeDevice(handle)

	var value C.int32_t
	if code := C.ha_get_device_stat(handle, C.HA_STAT_PING, C.size_t(C.sizeof_int32_t), unsafe.Pointer(&value)); code != C.HA_SUCCESS {
		return fmt.Errorf("ping device %d: %s", deviceIndex, errorString(code))
	}
	if value != 1 {
		return fmt.Errorf("ping device %d: unexpected value %d", deviceIndex, int32(value))
	}
	return nil
}

func (HADriver) GetTemperature(deviceIndex int) (int32, error) {
	handle, err := openDevice(deviceIndex)
	if err != nil {
		return 0, err
	}
	defer closeDevice(handle)

	var temp C.int32_t
	if code := C.ha_get_device_stat(handle, C.HA_STAT_TEMPERATURE, C.size_t(C.sizeof_int32_t), unsafe.Pointer(&temp)); code != C.HA_SUCCESS {
		return 0, fmt.Errorf("get temperature for device %d: %s", deviceIndex, errorString(code))
	}
	return int32(temp), nil
}

func openDevice(deviceIndex int) (C.ha_handle_t, error) {
	var code C.ha_error_t
	handle := C.ha_open_device(C.int(deviceIndex), 0, &code)
	if handle <= 0 {
		return 0, fmt.Errorf("open device %d: %s", deviceIndex, errorString(code))
	}
	return handle, nil
}

func closeDevice(handle C.ha_handle_t) {
	_ = C.ha_close_device(handle)
}

func errorString(code C.ha_error_t) string {
	return C.GoString(C.ha_strerror(code))
}
