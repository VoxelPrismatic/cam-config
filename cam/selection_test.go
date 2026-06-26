package cam

import "testing"

func TestFindResolutionLabelIndex(t *testing.T) {
	resolutions := []V42L_Resolution{
		{Width: 1920, Height: 1080},
		{Width: 1280, Height: 720},
	}

	if got := findResolutionLabelIndex(resolutions, "1280×720"); got != 1 {
		t.Fatalf("expected index 1, got %d", got)
	}
	if got := findResolutionLabelIndex(resolutions, "640×480"); got != 0 {
		t.Fatalf("expected fallback index 0, got %d", got)
	}
}

func TestFindFPSLabelIndex(t *testing.T) {
	rates := []float32{60, 30, 24}

	if got := findFPSLabelIndex(rates, "30 fps"); got != 1 {
		t.Fatalf("expected index 1, got %d", got)
	}
	if got := findFPSLabelIndex(rates, "25 fps"); got != 0 {
		t.Fatalf("expected fallback index 0, got %d", got)
	}
}