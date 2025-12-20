package infnoise

import (
	"errors"
	"sync"

	"golang.org/x/crypto/sha3"
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
	Mask = 0xED

	BufLen  = 512
	IOBatch = BufLen * 64

	WhitenedChunkSize = 2048
)

// Device represents a connection to an Infinite Noise TRNG hardware unit.
type Device struct {
	mu      sync.Mutex
	usbDev  *usbHandle
	running bool

	outPattern []byte
	outBulk    []byte
	inBulk     []byte

	sponge sha3.ShakeHash

	pool        []byte
	poolBuf     []byte
	rawPool     []byte
	rawFetchBuf []byte
}

// New initializes a new Infinite Noise device with default internal buffers.
func New() *Device {
	d := &Device{
		outPattern: make([]byte, BufLen),
		outBulk:    make([]byte, IOBatch),
		inBulk:     make([]byte, IOBatch),

		sponge:      sha3.NewCShake256(nil, []byte("infnoise")),
		poolBuf:     make([]byte, WhitenedChunkSize),
		rawPool:     make([]byte, 0, WhitenedChunkSize),
		rawFetchBuf: make([]byte, WhitenedChunkSize),
	}

	for i := range BufLen {
		if i&1 == 1 {
			d.outPattern[i] = (1 << SWEN2)
		} else {
			d.outPattern[i] = (1 << SWEN1)
		}

		d.outPattern[i] |= makeAddress(uint8(i & 0x0f))
	}

	for off := 0; off < len(d.outBulk); off += BufLen {
		copy(d.outBulk[off:off+BufLen], d.outPattern)
	}

	return d
}

// Start opens the USB connection and initializes the device into synchronous bitbang mode.
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

// Read implements io.Reader, filling p with cryptographically whitened entropy.
func (d *Device) Read(p []byte) (n int, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return 0, errors.New("device not started")
	}

	for n < len(p) {
		// 1. Drain whitened pool
		if len(d.pool) > 0 {
			todo := min(len(d.pool), len(p)-n)

			copy(p[n:], d.pool[:todo])
			d.pool = d.pool[todo:]

			n += todo

			continue
		}

		rawNeeded := WhitenedChunkSize - len(d.rawPool)
		if rawNeeded > 0 {
			rn, rerr := d.readRawLocked(d.rawFetchBuf[:rawNeeded])
			if rerr != nil {
				return n, rerr
			}

			d.rawPool = append(d.rawPool, d.rawFetchBuf[:rn]...)
		}

		if len(d.rawPool) >= WhitenedChunkSize {
			d.sponge.Write(d.rawPool[:WhitenedChunkSize])

			clone := d.sponge.Clone()
			clone.Read(d.poolBuf)

			d.rawPool = d.rawPool[:0]
			d.pool = d.poolBuf
		}
	}

	return n, nil
}

// ReadRaw fills p with the direct, unwhitened bitstream from the hardware.
func (d *Device) ReadRaw(p []byte) (n int, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.readRawLocked(p)
}

// Close stops the device and releases the underlying USB handle.
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

func (d *Device) readRawLocked(p []byte) (n int, err error) {
	if !d.running {
		return 0, errors.New("device not started")
	}

	for n < len(p) {
		needOut := len(p) - n

		needIn := min(needOut*8, len(d.inBulk))

		needIn &= ^7
		if needIn == 0 {
			return n, nil
		}

		err := d.usbDev.write(d.outBulk[:needIn])
		if err != nil {
			return n, err
		}

		err = d.usbDev.read(d.inBulk[:needIn])
		if err != nil {
			return n, err
		}

		outCount := min(needIn/8, needOut)

		in := d.inBulk[:needIn]
		out := p[n : n+outCount]

		for i := range outCount {
			base := i * 8

			var b uint8

			for j := range 8 {
				val := in[base+j]

				evenBit := (val >> COMP2) & 1
				oddBit := (val >> COMP1) & 1

				if (j & 1) == 1 {
					b = (b << 1) | oddBit
				} else {
					b = (b << 1) | evenBit
				}
			}

			out[i] = b
		}

		n += outCount
	}

	return n, nil
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
