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
	sioSetLatency  = 0x09
	sioResetSio    = 0x0000
	sioPurgeRx     = 0x0001
	sioPurgeTx     = 0x0002

	reqOutVendor = 0x40

	defaultTimeoutMS = 5000
	defaultIface     = 0
	epInAddr         = 0x81
	epOutAddr        = 0x02

	transferChunk = 512
)

type usbHandle struct {
	ctx  *C.libusb_context
	devh *C.libusb_device_handle

	iface int
	epIn  C.uchar
	epOut C.uchar

	maxPacket int

	rawBuf []byte
	rxBuf  []byte
}

func openUSB(vid, pid uint16) (*usbHandle, error) {
	h := &usbHandle{
		iface: defaultIface,
		epIn:  C.uchar(epInAddr),
		epOut: C.uchar(epOutAddr),
	}

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

	st = C.libusb_claim_interface(h.devh, C.int(h.iface))
	if st != 0 {
		h.close()

		return nil, usbErr(st)
	}

	h.maxPacket = 64

	dev := C.libusb_get_device(h.devh)
	if dev != nil {
		mps := int(C.libusb_get_max_packet_size(dev, h.epIn))
		if mps > 0 {
			h.maxPacket = mps
		}
	}

	h.rawBuf = make([]byte, 4096)

	h.rxBuf = make([]byte, 0, 64*1024)

	h.ctrlOut(sioReset, sioResetSio)
	h.ctrlOut(sioReset, sioPurgeRx)
	h.ctrlOut(sioReset, sioPurgeTx)
	h.ctrlOut(sioSetBitMode, 0)
	h.ctrlOut(sioSetLatency, 2)

	time.Sleep(10 * time.Millisecond)

	err := h.setBaudRate(30000)
	if err != nil {
		h.close()

		return nil, err
	}

	return h, nil
}

func (h *usbHandle) setBitMode(mask byte, mode byte) error {
	val := uint16(mask) | (uint16(mode) << 8)

	err := h.ctrlOut(sioSetBitMode, val)
	if err != nil {
		return err
	}

	h.ctrlOut(sioReset, sioPurgeRx)
	h.ctrlOut(sioReset, sioPurgeTx)

	h.rxBuf = h.rxBuf[:0]

	return nil
}

func (h *usbHandle) write(data []byte) error {
	if len(data) > 0 && len(h.rxBuf) > 0 {
		h.rxBuf = h.rxBuf[:0]
	}

	for off := 0; off < len(data); off += transferChunk {
		end := min(off+transferChunk, len(data))

		chunkLen := end - off

		var xfer C.int

		st := C.libusb_bulk_transfer(
			h.devh, h.epOut,
			(*C.uchar)(unsafe.Pointer(&data[off])),
			C.int(chunkLen),
			&xfer,
			defaultTimeoutMS,
		)

		if st != 0 {
			return usbErr(st)
		}

		neededPayload := chunkLen

		for neededPayload > 0 {
			st = C.libusb_bulk_transfer(
				h.devh, h.epIn,
				(*C.uchar)(unsafe.Pointer(&h.rawBuf[0])),
				C.int(len(h.rawBuf)),
				&xfer,
				defaultTimeoutMS,
			)

			if st != 0 {
				return usbErr(st)
			}

			nRaw := int(xfer)
			if nRaw == 0 {
				continue
			}

			payloadBytes := h.extractPayload(h.rawBuf[:nRaw])

			h.rxBuf = append(h.rxBuf, payloadBytes...)

			neededPayload -= len(payloadBytes)
		}
	}

	return nil
}

func (h *usbHandle) read(dst []byte) error {
	if len(dst) == 0 {
		return nil
	}

	if len(h.rxBuf) < len(dst) {
		return fmt.Errorf("read underrun: want %d bytes, have %d (device sync lost?)", len(dst), len(h.rxBuf))
	}

	copy(dst, h.rxBuf[:len(dst)])

	if len(dst) == len(h.rxBuf) {
		h.rxBuf = h.rxBuf[:0]
	} else {
		copy(h.rxBuf, h.rxBuf[len(dst):])
		h.rxBuf = h.rxBuf[:len(h.rxBuf)-len(dst)]
	}

	return nil
}

func (h *usbHandle) extractPayload(raw []byte) []byte {
	if len(raw) < 2 {
		return nil
	}

	mps := h.maxPacket
	dest := raw[:0]

	for off := 0; off < len(raw); off += mps {
		chunkEnd := min(off+mps, len(raw))

		if chunkEnd-off <= 2 {
			continue
		}

		dest = append(dest, raw[off+2:chunkEnd]...)
	}

	return dest
}

func (h *usbHandle) close() error {
	if h.devh != nil {
		h.ctrlOut(sioSetBitMode, 0)

		C.libusb_release_interface(h.devh, C.int(h.iface))
		C.libusb_close(h.devh)

		h.devh = nil
	}

	if h.ctx != nil {
		C.libusb_exit(h.ctx)

		h.ctx = nil
	}

	return nil
}

func (h *usbHandle) ctrlOut(req uint8, val uint16) error {
	idx := uint16(h.iface + 1)

	st := C.libusb_control_transfer(
		h.devh, reqOutVendor, C.uint8_t(req), C.uint16_t(val), C.uint16_t(idx),
		nil, 0, defaultTimeoutMS,
	)

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
