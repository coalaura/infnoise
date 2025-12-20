package infnoise

import (
	"math"
	"sync"
)

// HealthCheck implements the official Infinite Noise health monitoring algorithm.
type HealthCheck struct {
	mu sync.Mutex

	counts [128][2]uint32

	totalBits  uint64
	window     uint64
	entropySum float64

	TargetEntropy float64
	Tolerance     float64
}

// Add processes raw bytes and updates the entropy estimate.
func (h *HealthCheck) Add(data []byte) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	var history uint8

	for _, b := range data {
		for i := range 8 {
			bit := (b >> (7 - i)) & 1

			c0 := float64(h.counts[history][0])
			c1 := float64(h.counts[history][1])

			total := c0 + c1

			if total > 0 {
				prob := 0.5

				if bit == 0 {
					prob = c0 / total
				} else {
					prob = c1 / total
				}

				if prob > 0 {
					h.entropySum += -math.Log2(prob)
				}
			}

			h.counts[history][bit]++

			history = ((history << 1) | bit) & 0x7F

			h.totalBits++
		}
	}

	return h.IsHealthy()
}

// IsHealthy determines if the hardware is performing within expected physical parameters.
func (h *HealthCheck) IsHealthy() bool {
	if h.totalBits < h.window {
		return true
	}

	actual := h.entropySum / float64(h.totalBits)
	diff := math.Abs(actual - h.TargetEntropy)

	return diff <= (h.TargetEntropy * h.Tolerance)
}

// EstimatedEntropy returns the current calculated Shannon entropy per bit.
func (h *HealthCheck) EstimatedEntropy() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.totalBits == 0 {
		return 0
	}

	return h.entropySum / float64(h.totalBits)
}
