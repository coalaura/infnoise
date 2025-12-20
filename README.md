# infnoise

A high-performance Go port for the [Infinite Noise TRNG](https://github.com/waywardgeek/infnoise) USB device.

## Features
- **Cross-Platform**: Optimized drivers for both Windows (D2XX) and Linux (libusb-1.0).
- **High Throughput**: Achieves hardware limit (~60 KB/s raw) using asynchronous ring-buffering on Linux and native D2XX descriptors on Windows.
- **Zero-Alloc Path**: Optimized internal loops to minimize GC pressure during continuous entropy extraction.

## Requirements
- **Windows**: Requires `ftd2xx.dll` (usually installed with FTDI drivers) in your system path.
- **Linux**: Requires `libusb-1.0` development headers (`libusb-1.0-0-dev` on Debian/Ubuntu).

## Usage

```go
package main

import (
    "fmt"
    "github.com/coalaura/infnoise"
)

func main() {
    dev := infnoise.New()

    err := dev.Start()
    if err != nil {
        panic(err)
    }

    defer dev.Close()

    buf := make([]byte, 32768)

    n, err := dev.ReadRaw(buf)
    if err != nil {
        panic(err)
    }

    fmt.Printf("Read %d bytes of raw entropy\n", n)
}
```

## Implementation Details
- **Sync Bitbang**: The device is driven in Synchronous Bitbang mode.
- **Async Reader (Linux)**: To prevent USB stall/deadlock during large writes, the Linux implementation uses a background goroutine and a 64KB ring buffer to constantly drain the IN endpoint.
- **D2XX (Windows)**: Uses `syscall` to interface directly with the FTDI driver for maximum performance without CGO.

## Benchmarks
Tested on AMD Ryzen 9 9950X3D:
- **Throughput**: ~60 KB/s (almost hardware limit)
- **Latency**: ~2ms (configured via LatencyTimer)