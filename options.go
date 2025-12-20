package infnoise

type options struct {
	targetEntropy float64
	tolerance     float64
	window        uint64
}

type option func(*options)

// WithTargetEntropy overrides the theoretical entropy target (default 0.864).
func WithTargetEntropy(bits float64) option {
	return func(o *options) {
		o.targetEntropy = bits
	}
}

// WithTolerance sets the allowed deviation from the target (default 0.05).
func WithTolerance(percent float64) option {
	return func(o *options) {
		o.tolerance = percent
	}
}

// WithHealthWindow sets the number of bits required before the health check begins enforcing the tolerance (default 80,000).
func WithHealthWindow(bits uint64) option {
	return func(o *options) {
		o.window = bits
	}
}
