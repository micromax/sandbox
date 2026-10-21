package main

import "testing"

func TestParseSize(t *testing.T) {
	valid := map[string]uint64{
		"1048576": 1 << 20,
		"64M":     64 << 20,
		"64m":     64 << 20,
		"64MB":    64 << 20,
		"64MiB":   64 << 20,
		" 64 M ":  64 << 20,
		"512K":    512 << 10,
		"512KiB":  512 << 10,
		"1G":      1 << 30,
		"2gib":    2 << 30,
		"100B":    100,
	}
	for in, want := range valid {
		got, err := parseSize(in)
		if err != nil {
			t.Errorf("parseSize(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}

	invalid := []string{
		"",
		"   ",
		"M",
		"abc",
		"-5M",
		"+5M",
		"1.5G",
		"0",
		"0M",
		"64X",
		"64MBB",
		"64 M B",
		"18446744073709551616", // overflows uint64
		"17179869184G",         // 2^34 GiB overflows uint64 once multiplied
		"99999999999999999999999999M",
		"\x0064M",
	}
	for _, in := range invalid {
		if got, err := parseSize(in); err == nil {
			t.Errorf("parseSize(%q) = %d, want error", in, got)
		}
	}
}

func TestParseSizeLargestRepresentable(t *testing.T) {
	// 2^34 - 1 GiB still fits; it is the largest whole-GiB value that does.
	got, err := parseSize("17179869183G")
	if err != nil {
		t.Fatal(err)
	}
	if want := uint64(17179869183) << 30; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}
