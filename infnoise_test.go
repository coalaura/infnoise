package infnoise

import (
	"math/bits"
	"testing"
)

const (
	testBytes = 32 * 1024  // 32 KiB
	testChunk = 256 * 1024 // 256 KiB
)

func openDevice(t testing.TB) *Device {
	t.Helper()

	dv := New()

	err := dv.Start()
	if err != nil {
		dv.Close()

		t.Skipf("unable to start device (is it plugged in / driver installed?): %v", err)
	}

	t.Cleanup(func() {
		dv.Close()
	})

	return dv
}

func TestReadRaw(t *testing.T) {
	dv := openDevice(t)

	runEntropyTest(t, dv.ReadRaw, "raw")
}

func TestRead(t *testing.T) {
	dv := openDevice(t)

	runEntropyTest(t, dv.Read, "whitened")
}

func runEntropyTest(t *testing.T, readFn func([]byte) (int, error), label string) {
	t.Helper()

	buf1 := make([]byte, testBytes)
	buf2 := make([]byte, testBytes)

	n, err := readFn(buf1)
	if err != nil {
		t.Fatal(err)
	}

	if n != len(buf1) {
		t.Fatalf("%s: read only %d bytes, want %d", label, n, len(buf1))
	}

	n, err = readFn(buf2)
	if err != nil {
		t.Fatal(err)
	}

	if n != len(buf2) {
		t.Fatalf("%s: read only %d bytes, want %d", label, n, len(buf2))
	}

	var (
		sameAsFirst int
		ones        int
	)

	unique := make(map[byte]struct{}, 256)

	for i := range testBytes {
		if buf2[i] == buf1[i] {
			sameAsFirst++
		}

		unique[buf1[i]] = struct{}{}

		ones += bits.OnesCount8(buf1[i])
	}

	if len(unique) < 200 && label == "whitened" {
		t.Fatalf("%s: too few unique byte values (%d); whitening failed", label, len(unique))
	} else if len(unique) < 8 {
		t.Fatalf("%s: too few unique byte values (%d); device stuck", label, len(unique))
	}

	eqFrac := float64(sameAsFirst) / float64(testBytes)
	if eqFrac > 0.05 {
		t.Fatalf("%s: consecutive blocks too similar: %.2f%% (want < 5%%)", label, 100*eqFrac)
	}

	totalBits := float64(testBytes * 8)
	oneFrac := float64(ones) / totalBits

	low, high := 0.45, 0.55
	if label == "whitened" {
		low, high = 0.49, 0.51
	}

	if oneFrac < low || oneFrac > high {
		t.Fatalf("%s: bit bias suspicious: ones fraction %.4f (want [%.2f, %.2f])", label, oneFrac, low, high)
	}

	t.Logf("%s stats: uniqueBytes=%d ones=%.2f%% eqPos=%.2f%%", label, len(unique), 100*oneFrac, 100*eqFrac)
}

func BenchmarkReadRawThroughput(b *testing.B) {
	dv := openDevice(b)

	runBenchmark(b, dv.ReadRaw)
}

func BenchmarkReadThroughput(b *testing.B) {
	dv := openDevice(b)

	runBenchmark(b, dv.Read)
}

func runBenchmark(b *testing.B, readFn func([]byte) (int, error)) {
	b.Helper()

	buf := make([]byte, testChunk)

	for range 3 {
		n, err := readFn(buf)
		if err != nil {
			b.Fatal(err)
		}

		if n != len(buf) {
			b.Fatalf("read only %d raw bytes, want %d", n, len(buf))
		}
	}

	b.ReportAllocs()
	b.SetBytes(testChunk)
	b.ResetTimer()

	for b.Loop() {
		n, err := readFn(buf)
		if err != nil {
			b.Fatal(err)
		}

		if n != len(buf) {
			b.Fatalf("short read: %d < %d", n, len(buf))
		}
	}

	b.StopTimer()

	sec := b.Elapsed().Seconds()
	if sec <= 0 {
		return
	}

	totalBytes := float64(int64(b.N) * testChunk)

	kBps := (totalBytes / 1000.0) / sec
	kbps := (totalBytes * 8.0 / 1000.0) / sec

	b.ReportMetric(kBps, "KB/s")
	b.ReportMetric(kbps, "Kbps")
}
