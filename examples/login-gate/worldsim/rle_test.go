package worldsim

import (
	"math/rand"
	"testing"
)

func TestGridRLERoundTrip(t *testing.T) {
	cases := []string{
		"",
		"0",
		"00000",
		"012012012",
		"000111222000",
	}
	for _, s := range cases {
		if got, err := DecodeGridRLE(EncodeGridRLE(s), len(s)); err != nil || got != s {
			t.Fatalf("RLE roundtrip %q: got %q err %v", s, got, err)
		}
		if got, err := DecodeGridBase64(EncodeGridBase64(s), len(s)); err != nil || got != s {
			t.Fatalf("base64 roundtrip %q: got %q err %v", s, got, err)
		}
	}
}

func TestGridRLERoundTripRealMap(t *testing.T) {
	rng := rand.New(rand.NewSource(12345))
	gm := GenerateMap(rng)
	raw := gm.MapString

	got, err := DecodeGridBase64(EncodeGridBase64(raw), len(raw))
	if err != nil {
		t.Fatalf("decode real map: %v", err)
	}
	if got != raw {
		t.Fatalf("real map roundtrip mismatch (len %d vs %d)", len(got), len(raw))
	}
}

func TestGridRLECompressionRatio(t *testing.T) {
	rng := rand.New(rand.NewSource(12345))
	gm := GenerateMap(rng)
	raw := gm.MapString
	enc := EncodeGridBase64(raw)
	t.Logf("map:    raw=%d bytes -> rle+base64=%d bytes (%.1fx)", len(raw), len(enc), float64(len(raw))/float64(len(enc)))

	// 城堡：8×8 实心块铺满 1/8，行程极长，用真实 SeedCastles 测压缩率。
	g := NewGame(gm.Grid, rng, 1)
	g.SeedCastles()
	b := make([]byte, MapW*MapH)
	for i := range b {
		b[i] = '0'
	}
	for _, c := range g.AllCastleCells() {
		b[c.Y*MapW+c.X] = '1'
	}
	castle := string(b)
	cenc := EncodeGridBase64(castle)
	t.Logf("castle: raw=%d bytes -> rle+base64=%d bytes (%.1fx)", len(castle), len(cenc), float64(len(castle))/float64(len(cenc)))
}
