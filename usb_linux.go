//go:build linux
// +build linux

package infnoise

/*
#cgo linux pkg-config: libusb-1.0
#include <libusb-1.0/libusb.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
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
	epInAddr         = 0x81
	epOutAddr        = 0x02

	ringBufferSize = 64 * 1024
)

type usbHandle struct {
	ctx  *C.libusb_context
	devh *C.libusb_device_handle

	iface int
	epIn  C.uchar
	epOut C.uchar

	maxPacket int

	mu     sync.Mutex
	cond   *sync.Cond
	closed bool
	wg     sync.WaitGroup

	rBuf  []byte
	rHead int
	rTail int
	count int
}

func openUSB(vid, pid uint16) (*usbHandle, error) {
	h := &usbHandle{
		iface: 0,
		epIn:  C.uchar(epInAddr),
		epOut: C.uchar(epOutAddr),
		rBuf:  make([]byte, ringBufferSize),
	}

	h.cond = sync.NewCond(&h.mu)

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

	h.ctrlOut(sioReset, sioResetSio)
	h.ctrlOut(sioReset, sioPurgeRx)
	h.ctrlOut(sioReset, sioPurgeTx)
	h.ctrlOut(sioSetBitMode, 0)
	h.ctrlOut(sioSetLatency, 2)

	time.Sleep(10 * time.Millisecond)

	err = h.setBaudRate(30000)
	if err != nil {
		h.close()
		return nil, err
	}

	h.wg.Add(1)

	go h.readerLoop()

	return h, nil
}

func (h *usbHandle) setBitMode(mask byte, mode byte) error {
	val := uint16(mask) | (uint16(mode) << 8)

	err := h.ctrlOut(sioSetBitMode, val)
	if err != nil {
		return err
	}

	h.mu.Lock()

	h.ctrlOut(sioReset, sioPurgeRx)
	h.ctrlOut(sioReset, sioPurgeTx)

	h.rHead = 0
	h.rTail = 0
	h.count = 0

	h.mu.Unlock()

	return nil
}

func (h *usbHandle) write(data []byte) error {
	var total int

	for total < len(data) {
		var xfer C.int

		toWrite := len(data) - total

		st := C.libusb_bulk_transfer(
			h.devh, h.epOut,
			(*C.uchar)(unsafe.Pointer(&data[total])),
			C.int(toWrite),
			&xfer,
			defaultTimeoutMS,
		)

		if st != 0 {
			return usbErr(st)
		}

		if xfer <= 0 {
			return fmt.Errorf("short write: %d", xfer)
		}

		total += int(xfer)
	}

	return nil
}

func (h *usbHandle) read(dst []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	totalRead := 0

	for totalRead < len(dst) {
		for h.count == 0 {
			if h.closed {
				return errors.New("usb device closed")
			}

			h.cond.Wait()
		}

		available := h.count
		end := min(h.rTail+available, len(h.rBuf))
		contiguous := end - h.rTail

		needed := len(dst) - totalRead
		toCopy := min(contiguous, needed)

		copy(dst[totalRead:], h.rBuf[h.rTail:h.rTail+toCopy])

		h.rTail = (h.rTail + toCopy) % len(h.rBuf)

		h.count -= toCopy
		totalRead += toCopy
	}

	return nil
}

func (h *usbHandle) readerLoop() {
	defer h.wg.Done()

	scratch := make([]byte, 4096)
	mps := h.maxPacket

	for {
		var xfer C.int

		st := C.libusb_bulk_transfer(
			h.devh, h.epIn,
			(*C.uchar)(unsafe.Pointer(&scratch[0])),
			C.int(len(scratch)),
			&xfer,
			100,
		)

		if st == C.LIBUSB_ERROR_TIMEOUT {
			h.mu.Lock()

			if h.closed {
				h.mu.Unlock()

				return
			}

			h.mu.Unlock()

			continue
		}
		if st != 0 {
			h.mu.Lock()

			h.closed = true
			h.cond.Broadcast()

			h.mu.Unlock()

			return
		}

		n := int(xfer)
		if n <= 0 {
			continue
		}

		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()

			return
		}

		for i := 0; i < n; i += mps {
			pktEnd := min(i+mps, n)

			if pktEnd-i > 2 {
				payload := scratch[i+2 : pktEnd]
				pLen := len(payload)

				if h.count+pLen <= len(h.rBuf) {
					end := h.rHead + pLen

					if end <= len(h.rBuf) {
						copy(h.rBuf[h.rHead:], payload)
					} else {
						firstPart := len(h.rBuf) - h.rHead

						copy(h.rBuf[h.rHead:], payload[:firstPart])
						copy(h.rBuf[0:], payload[firstPart:])
					}

					h.rHead = (h.rHead + pLen) % len(h.rBuf)
					h.count += pLen
				}
			}
		}

		h.cond.Signal()
		h.mu.Unlock()
	}
}

func (h *usbHandle) close() error {
	h.mu.Lock()

	if !h.closed {
		h.closed = true
		h.cond.Broadcast()
	}

	h.mu.Unlock()

	h.wg.Wait()

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

func (h *usbHandle) setLatencyTimer(ms byte) error {
	return h.ctrlOut(sioSetLatency, uint16(ms))
}

func usbErr(st C.int) error {
	if st == 0 {
		return nil
	}

	return fmt.Errorf("libusb %s (%d)", C.GoString(C.libusb_error_name(st)), int(st))
}
