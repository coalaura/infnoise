package infnoise

import (
	"math/bits"
	"testing"
)

const (
	testBytes = 32 * 1024
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

	buf1 := make([]byte, testBytes)
	buf2 := make([]byte, testBytes)

	n, err := dv.ReadRaw(buf1)
	if err != nil {
		t.Fatal(err)
	}

	if n != len(buf1) {
		t.Fatalf("read only %d raw bytes, want %d", n, len(buf1))
	}

	n, err = dv.ReadRaw(buf2)
	if err != nil {
		t.Fatal(err)
	}

	if n != len(buf2) {
		t.Fatalf("read only %d raw bytes, want %d", n, len(buf2))
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

	if len(unique) < 8 {
		t.Fatalf("too few unique byte values (%d); device/data may be stuck", len(unique))
	}

	eqFrac := float64(sameAsFirst) / float64(testBytes)
	if eqFrac > 0.05 {
		t.Fatalf("consecutive blocks too similar: equal positions %.2f%% (want < 5%%)", 100*eqFrac)
	}

	totalBits := float64(testBytes * 8)
	oneFrac := float64(ones) / totalBits
	if oneFrac < 0.45 || oneFrac > 0.55 {
		t.Fatalf("bit bias suspicious: ones fraction %.4f (want in [0.45, 0.55])", oneFrac)
	}

	t.Logf("raw stats: uniqueBytes=%d ones=%.2f%% eqPos=%.2f%%", len(unique), 100*oneFrac, 100*eqFrac)
}

func BenchmarkReadRawThroughput(b *testing.B) {
	dv := openDevice(b)

	buf := make([]byte, testChunk)

	for range 3 {
		n, err := dv.ReadRaw(buf)
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
		n, err := dv.ReadRaw(buf)
		if err != nil {
			b.Fatal(err)
		}

		if n != len(buf) {
			b.Fatalf("read only %d raw bytes, want %d", n, len(buf))
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
