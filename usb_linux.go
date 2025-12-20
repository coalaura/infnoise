//go:build linux
// +build linux

package infnoise

/*
#cgo linux pkg-config: libusb-1.0
#include <libusb-1.0/libusb.h>
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

const (
	sioReset       = 0x00
	sioSetBaudRate = 0x03
	sioSetBitMode  = 0x0B
	sioResetSio    = 0x0000
	sioPurgeRx     = 0x0001
	sioPurgeTx     = 0x0002
	reqOutVendor   = 0x40
	defaultTO      = 5000
	epInAddr       = 0x81
	epOutAddr      = 0x02

	ioChunkSize = 512
)

type usbHandle struct {
	ctx  *C.libusb_context
	devh *C.libusb_device_handle

	iface int
	epIn  C.uchar
	epOut C.uchar

	maxPacket int
	rawBuf    []byte
	rx        []byte
	rxHead    int
	rxTail    int
}

func openUSB(vid, pid uint16) (*usbHandle, error) {
	h := &usbHandle{}

	st := C.libusb_init(&h.ctx)
	if st != 0 {
		return nil, usbErr(st)
	}

	h.devh = C.libusb_open_device_with_vid_pid(h.ctx, C.uint16_t(vid), C.uint16_t(pid))
	if h.devh == nil {
		h.close()

		return nil, fmt.Errorf("device 0x%04x:0x%04x not found", vid, pid)
	}

	C.libusb_set_auto_detach_kernel_driver(h.devh, 1)

	st = C.libusb_set_configuration(h.devh, 1)
	if st != 0 && st != C.LIBUSB_ERROR_BUSY {
		h.close()

		return nil, usbErr(st)
	}

	h.iface = 0
	h.epIn = C.uchar(epInAddr)
	h.epOut = C.uchar(epOutAddr)

	st = C.libusb_claim_interface(h.devh, C.int(h.iface))
	if st != 0 {
		h.close()

		return nil, usbErr(st)
	}

	h.maxPacket = 64
	h.rawBuf = make([]byte, 8192)
	h.rx = make([]byte, 32768)

	h.ctrlOut(sioReset, sioResetSio)
	h.ctrlOut(sioReset, sioPurgeRx)
	h.ctrlOut(sioReset, sioPurgeTx)
	h.ctrlOut(sioSetBitMode, 0)

	time.Sleep(10 * time.Millisecond)

	h.setBaudRate(30000)

	return h, nil
}

func (h *usbHandle) setBitMode(mask byte, mode byte) error {
	val := uint16(mask) | (uint16(mode) << 8)

	err := h.ctrlOut(sioSetBitMode, val)
	if err != nil {
		return err
	}

	h.rxHead = 0
	h.rxTail = 0

	return nil
}

func (h *usbHandle) write(data []byte) error {
	for i := 0; i < len(data); i += ioChunkSize {
		end := i + ioChunkSize

		if end > len(data) {
			end = len(data)
		}

		var xfer C.int

		st := C.libusb_bulk_transfer(h.devh, h.epOut, (*C.uchar)(unsafe.Pointer(&data[i])), C.int(end-i), &xfer, defaultTO)
		if st != 0 {
			return usbErr(st)
		}

		err := h.fillRX(h.available() + int(xfer))
		if err != nil {
			return err
		}
	}

	return nil
}

func (h *usbHandle) read(dst []byte) error {
	err := h.fillRX(len(dst))
	if err != nil {
		return err
	}

	copy(dst, h.rx[h.rxHead:h.rxHead+len(dst)])

	h.rxHead += len(dst)

	return nil
}

func (h *usbHandle) available() int {
	return h.rxTail - h.rxHead
}

func (h *usbHandle) fillRX(n int) error {
	for h.available() < n {
		if h.rxHead > (len(h.rx) / 2) {
			copy(h.rx, h.rx[h.rxHead:h.rxTail])

			h.rxTail -= h.rxHead
			h.rxHead = 0
		}

		var xfer C.int

		st := C.libusb_bulk_transfer(h.devh, h.epIn, (*C.uchar)(unsafe.Pointer(&h.rawBuf[0])), C.int(len(h.rawBuf)), &xfer, defaultTO)
		if st != 0 {
			return usbErr(st)
		}

		nraw := int(xfer)
		mps := h.maxPacket

		for off := 0; off < nraw; off += mps {
			sz := mps
			if nraw-off < sz {
				sz = nraw - off
			}

			if sz > 2 {
				payloadLen := sz - 2

				if h.rxTail+payloadLen > len(h.rx) {
					return fmt.Errorf("rx buffer overflow")
				}

				copy(h.rx[h.rxTail:], h.rawBuf[off+2:off+sz])

				h.rxTail += payloadLen
			}
		}
	}

	return nil
}

func (h *usbHandle) close() error {
	if h.devh != nil {
		h.ctrlOut(sioSetBitMode, 0)

		C.libusb_release_interface(h.devh, C.int(h.iface))
		C.libusb_close(h.devh)
	}

	if h.ctx != nil {
		C.libusb_exit(h.ctx)
	}

	return nil
}

func (h *usbHandle) ctrlOut(req uint8, val uint16) error {
	st := C.libusb_control_transfer(h.devh, reqOutVendor, C.uint8_t(req), C.uint16_t(val), 1, nil, 0, defaultTO)
	if st < 0 {
		return usbErr(C.int(st))
	}

	return nil
}

func (h *usbHandle) setBaudRate(baud int) error {
	div := uint16(3000000 / baud)

	return h.ctrlOut(sioSetBaudRate, div)
}

func usbErr(st C.int) error {
	if st == 0 {
		return nil
	}

	return fmt.Errorf("libusb %s (%d)", C.GoString(C.libusb_error_name(st)), int(st))
}
