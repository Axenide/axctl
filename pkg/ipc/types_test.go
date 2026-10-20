package ipc

import "testing"

func TestLogicalSizeFromPhysical(t *testing.T) {
	tests := []struct {
		name  string
		w, h  int
		scale float64
		wantW int
		wantH int
	}{
		{name: "scale 2", w: 2880, h: 1800, scale: 2, wantW: 1440, wantH: 900},
		{name: "scale 1.25", w: 1920, h: 1080, scale: 1.25, wantW: 1536, wantH: 864},
		{name: "scale 1", w: 1920, h: 1080, scale: 1, wantW: 1920, wantH: 1080},
		{name: "zero scale falls back to identity", w: 1920, h: 1080, scale: 0, wantW: 1920, wantH: 1080},
		{name: "negative scale falls back to identity", w: 1920, h: 1080, scale: -1, wantW: 1920, wantH: 1080},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotW, gotH := LogicalSizeFromPhysical(tt.w, tt.h, tt.scale)
			if gotW != tt.wantW || gotH != tt.wantH {
				t.Fatalf("LogicalSizeFromPhysical(%d, %d, %v) = %d, %d; want %d, %d",
					tt.w, tt.h, tt.scale, gotW, gotH, tt.wantW, tt.wantH)
			}
		})
	}
}
