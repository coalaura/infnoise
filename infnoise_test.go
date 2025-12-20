package infnoise

import "testing"

func TestInfnoise(t *testing.T) {
	dv := New()

	defer dv.Close()

	err := dv.Start()
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 256)

	n, err := dv.ReadRaw(buf)
	if err != nil {
		t.Fatal(err)
	} else if n != 256 {
		t.Fatalf("read only %d raw bytes", n)
	}

	t.Log(buf)
}
