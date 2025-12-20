# infnoise

A high-performance Go port for the [Infinite Noise TRNG](https://github.com/waywardgeek/infnoise) USB device.

## Features
- **Cross-Platform**: Optimized drivers for Windows (D2XX) and Linux (libusb-1.0).
- **Self-Contained Linux Build**: Includes pre-compiled `libusb.a` for `amd64` and `arm64`; no system-wide libusb installation required for compilation.
- **Whitened Output**: SHA3-based whitening (cSHAKE256) for cryptographically secure entropy.
- **Health Monitoring**: Continuous real-time Shannon entropy estimation and hardware failure detection.
- **High Throughput**: Achieves full hardware limit (~60 KB/s) via asynchronous ring-buffering.

## Requirements
- **Windows**: Requires `ftd2xx.dll` (standard FTDI drivers) in system path. No CGO required.
- **Linux**: Bundled headers and static libraries included. Requires CGO for linking.

## Usage

```go
package main

import (
	"fmt"
	"github.com/coalaura/infnoise"
)

func main() {
	// Initialize with optional health check tuning
	dev := infnoise.New(
		infnoise.WithTargetEntropy(0.864),
		infnoise.WithTolerance(0.05),
	)

    err := dev.Start()
	if err != nil {
		panic(err)
	}

	defer dev.Close()

	// Read whitened entropy (io.Reader)
	buf := make([]byte, 32)

    _, err := dev.Read(buf)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Entropy: %x\n", buf)
}
```

## Implementation Details
- **Linux**: Uses a background reader goroutine and 64KB ring buffer to prevent USB stalls.
- **Windows**: Interfaces directly with `ftd2xx.dll` via `syscall` (Zero-CGO).
- **Whitening**: Uses `cSHAKE256` to process raw input in 2048-bit chunks.

## Benchmarks (AMD Ryzen 9 9950X3D)
| Mode | OS | Throughput | Bitrate | Allocations |
| :--- | :--- | :--- | :--- | :--- |
| **Raw** | Windows 11 | 59.65 KB/s | 477.2 Kbps | 4608 B/op (256 allocs) |
| **Whitened** | Windows 11 | 59.44 KB/s | 475.5 Kbps | 142352 B/op (1408 allocs) |
| **Raw** | Linux (WSL2) | 59.31 KB/s | 474.5 Kbps | 14512 B/op (3277 allocs) |
| **Whitened** | Linux (WSL2) | 58.76 KB/s | 470.1 Kbps | 146288 B/op (4001 allocs) |
