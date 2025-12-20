//go:build windows
// +build windows

package infnoise

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	ftd2xx = syscall.NewLazyDLL("ftd2xx.dll")

	pFT_CreateDeviceInfoList = ftd2xx.NewProc("FT_CreateDeviceInfoList")
	pFT_GetDeviceInfoDetail  = ftd2xx.NewProc("FT_GetDeviceInfoDetail")
	pFT_OpenEx               = ftd2xx.NewProc("FT_OpenEx")

	pFT_Close            = ftd2xx.NewProc("FT_Close")
	pFT_ResetDevice      = ftd2xx.NewProc("FT_ResetDevice")
	pFT_Purge            = ftd2xx.NewProc("FT_Purge")
	pFT_SetUSBParameters = ftd2xx.NewProc("FT_SetUSBParameters")
	pFT_SetChars         = ftd2xx.NewProc("FT_SetChars")
	pFT_SetFlowControl   = ftd2xx.NewProc("FT_SetFlowControl")
	pFT_SetTimeouts      = ftd2xx.NewProc("FT_SetTimeouts")
	pFT_SetLatencyTimer  = ftd2xx.NewProc("FT_SetLatencyTimer")
	pFT_SetBaudRate      = ftd2xx.NewProc("FT_SetBaudRate")
	pFT_SetBitMode       = ftd2xx.NewProc("FT_SetBitMode")

	pFT_Write = ftd2xx.NewProc("FT_Write")
	pFT_Read  = ftd2xx.NewProc("FT_Read")
)

const (
	FT_OK = 0

	FT_PURGE_RX = 1
	FT_PURGE_TX = 2

	FT_OPEN_BY_SERIAL_NUMBER = 1

	FT_FLOW_NONE = 0x0000
)

type usbHandle struct {
	ftHandle uintptr
}

func openUSB(vid, pid uint16) (*usbHandle, error) {
	err := ftd2xx.Load()
	if err != nil {
		return nil, fmt.Errorf("ftd2xx.dll not available: %w", err)
	}

	serial, err := findFirstDeviceSerial(vid, pid)
	if err != nil {
		return nil, err
	}

	serialZ, err := syscall.BytePtrFromString(serial)
	if err != nil {
		return nil, err
	}

	var handle uintptr

	st, _, _ := pFT_OpenEx.Call(uintptr(unsafe.Pointer(serialZ)), FT_OPEN_BY_SERIAL_NUMBER, uintptr(unsafe.Pointer(&handle)))
	if st != FT_OK {
		return nil, fmt.Errorf("FT_OpenEx(by serial=%q) failed: %d", serial, st)
	}

	h := &usbHandle{
		ftHandle: handle,
	}

	st, _, _ = pFT_ResetDevice.Call(h.ftHandle)
	if st != FT_OK {
		h.close()

		return nil, fmt.Errorf("FT_ResetDevice failed: %d", st)
	}

	st, _, _ = pFT_Purge.Call(h.ftHandle, FT_PURGE_RX|FT_PURGE_TX)
	if st != FT_OK {
		h.close()

		return nil, fmt.Errorf("FT_Purge failed: %d", st)
	}

	st, _, _ = pFT_SetUSBParameters.Call(h.ftHandle, 65536, 65536)
	if st != FT_OK {
		h.close()

		return nil, fmt.Errorf("FT_SetUSBParameters failed: %d", st)
	}

	st, _, _ = pFT_SetChars.Call(h.ftHandle, 0, 0, 0, 0)
	if st != FT_OK {
		h.close()

		return nil, fmt.Errorf("FT_SetChars failed: %d", st)
	}

	st, _, _ = pFT_SetFlowControl.Call(h.ftHandle, FT_FLOW_NONE, 0, 0)
	if st != FT_OK {
		h.close()

		return nil, fmt.Errorf("FT_SetFlowControl failed: %d", st)
	}

	st, _, _ = pFT_SetLatencyTimer.Call(h.ftHandle, 2)
	if st != FT_OK {
		h.close()

		return nil, fmt.Errorf("FT_SetLatencyTimer failed: %d", st)
	}

	st, _, _ = pFT_SetTimeouts.Call(h.ftHandle, 5000, 5000)
	if st != FT_OK {
		h.close()

		return nil, fmt.Errorf("FT_SetTimeouts failed: %d", st)
	}

	st, _, _ = pFT_SetBitMode.Call(h.ftHandle, 0, 0)
	if st != FT_OK {
		h.close()

		return nil, fmt.Errorf("FT_SetBitMode(reset) failed: %d", st)
	}

	time.Sleep(50 * time.Millisecond)

	st, _, _ = pFT_SetBaudRate.Call(h.ftHandle, 30000)
	if st != FT_OK {
		h.close()

		return nil, fmt.Errorf("FT_SetBaudRate failed: %d", st)
	}

	return h, nil
}

func (h *usbHandle) setBitMode(mask byte, mode byte) error {
	st, _, _ := pFT_SetBitMode.Call(h.ftHandle, uintptr(mask), uintptr(mode))
	if st != FT_OK {
		return fmt.Errorf("FT_SetBitMode(mask=0x%02x, mode=0x%02x) failed: %d", mask, mode, st)
	}

	buf := make([]byte, 64)

	err := h.writeExact(buf)
	if err != nil {
		return fmt.Errorf("prime write failed: %w", err)
	}

	err = h.readExact(buf)
	if err != nil {
		return fmt.Errorf("prime read failed: %w", err)
	}

	st, _, _ = pFT_Purge.Call(h.ftHandle, FT_PURGE_RX|FT_PURGE_TX)
	if st != FT_OK {
		return fmt.Errorf("FT_Purge(after bitmode) failed: %d", st)
	}

	return nil
}

func (h *usbHandle) write(data []byte) error {
	return h.writeExact(data)
}

func (h *usbHandle) read(data []byte) error {
	return h.readExact(data)
}

func (h *usbHandle) writeExact(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	var bytesWritten uint32

	st, _, _ := pFT_Write.Call(
		h.ftHandle,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&bytesWritten)),
	)

	if st != FT_OK {
		return fmt.Errorf("FT_Write failed: %d", st)
	}

	if int(bytesWritten) != len(data) {
		return fmt.Errorf("FT_Write short write: wrote %d, want %d", bytesWritten, len(data))
	}

	return nil
}

func (h *usbHandle) readExact(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	var total int

	for total < len(data) {
		need := len(data) - total

		var got uint32

		st, _, _ := pFT_Read.Call(
			h.ftHandle,
			uintptr(unsafe.Pointer(&data[total])),
			uintptr(need),
			uintptr(unsafe.Pointer(&got)),
		)

		if st != FT_OK {
			return fmt.Errorf("FT_Read failed: %d", st)
		}

		if got == 0 {
			return fmt.Errorf("FT_Read timeout/stall: got %d, want %d", total, len(data))
		}

		total += int(got)
	}

	return nil
}

func (h *usbHandle) close() error {
	if h.ftHandle != 0 {
		pFT_SetBitMode.Call(h.ftHandle, 0, 0)
		pFT_Close.Call(h.ftHandle)

		h.ftHandle = 0
	}

	return nil
}

func findFirstDeviceSerial(vid, pid uint16) (string, error) {
	var n uint32

	st, _, _ := pFT_CreateDeviceInfoList.Call(uintptr(unsafe.Pointer(&n)))
	if st != FT_OK {
		return "", fmt.Errorf("FT_CreateDeviceInfoList failed: %d", st)
	}

	if n == 0 {
		return "", errors.New("no FTDI devices found")
	}

	wantID := (uint32(vid) << 16) | uint32(pid)

	for i := range n {
		var (
			flags   uint32
			devType uint32
			id      uint32
			locID   uint32
		)

		serial := make([]byte, 16)
		desc := make([]byte, 64)

		var dummyHandle uintptr

		st, _, _ = pFT_GetDeviceInfoDetail.Call(
			uintptr(i),
			uintptr(unsafe.Pointer(&flags)),
			uintptr(unsafe.Pointer(&devType)),
			uintptr(unsafe.Pointer(&id)),
			uintptr(unsafe.Pointer(&locID)),
			uintptr(unsafe.Pointer(&serial[0])),
			uintptr(unsafe.Pointer(&desc[0])),
			uintptr(unsafe.Pointer(&dummyHandle)),
		)

		if st != FT_OK {
			continue
		}

		if id != wantID {
			continue
		}

		s := cString(serial)
		if s == "" {
			continue
		}

		return s, nil
	}

	return "", fmt.Errorf("no matching FTDI device found for VID=0x%04x PID=0x%04x", vid, pid)
}

func cString(b []byte) string {
	var n int

	for n < len(b) && b[n] != 0 {
		n++
	}

	return string(b[:n])
}
