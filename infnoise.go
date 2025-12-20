package infnoise

import (
	"errors"
	"sync"
)

const (
	COMP1 = 1
	COMP2 = 4
	SWEN1 = 2
	SWEN2 = 0

	ADDR0 = 3
	ADDR1 = 5
	ADDR2 = 6
	ADDR3 = 7

	// All bits are outputs except COMP1 and COMP2.
	// 0xFF &~(1<<1) &~(1<<4) == 0xED
	Mask   = 0xED
	BufLen = 512
)

type Device struct {
	mu      sync.Mutex
	usbDev  *usbHandle
	outBuf  []byte
	running bool
}

func New() *Device {
	d := &Device{
		outBuf: make([]byte, BufLen),
	}

	for i := range BufLen {
		if i&1 == 1 {
			d.outBuf[i] = (1 << SWEN2)
		} else {
			d.outBuf[i] = (1 << SWEN1)
		}

		d.outBuf[i] |= makeAddress(uint8(i & 0x0f))
	}

	return d
}

func (d *Device) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	handle, err := openUSB(0x0403, 0x6015)
	if err != nil {
		return err
	}

	err = handle.setBitMode(Mask, 0x04)
	if err != nil {
		handle.close()

		return err
	}

	d.usbDev = handle
	d.running = true

	return nil
}

// ReadRaw fills p with RAW bytes extracted from COMP2/COMP1.
func (d *Device) ReadRaw(p []byte) (n int, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return 0, errors.New("device not started")
	}

	inBuf := make([]byte, BufLen)

	for n < len(p) {
		err := d.usbDev.write(d.outBuf)
		if err != nil {
			return n, err
		}

		err = d.usbDev.read(inBuf)
		if err != nil {
			return n, err
		}

		for i := 0; i < BufLen/8 && n < len(p); i++ {
			var b uint8

			for j := 0; j < 8; j++ {
				val := inBuf[i*8+j]

				evenBit := (val >> COMP2) & 1 // COMP2
				oddBit := (val >> COMP1) & 1  // COMP1

				var bit uint8

				if (j & 1) == 1 {
					bit = oddBit
				} else {
					bit = evenBit
				}

				b = (b << 1) | bit
			}

			p[n] = b
			n++
		}
	}

	return n, nil
}

func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.running = false

	if d.usbDev != nil {
		err := d.usbDev.close()

		d.usbDev = nil

		return err
	}

	return nil
}

func makeAddress(addr uint8) uint8 {
	var value uint8

	if addr&1 != 0 {
		value |= 1 << ADDR0
	}

	if addr&2 != 0 {
		value |= 1 << ADDR1
	}

	if addr&4 != 0 {
		value |= 1 << ADDR2
	}

	if addr&8 != 0 {
		value |= 1 << ADDR3
	}

	return value
}
