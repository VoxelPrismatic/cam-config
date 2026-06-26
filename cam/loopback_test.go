package cam

import "testing"

func TestParseLoopbackDevice(t *testing.T) {
	dev, ok := parseLoopbackDevice("created /dev/video8\n/dev/video8\n")
	if !ok || dev != "/dev/video8" {
		t.Fatalf("unexpected device: %#v %v", dev, ok)
	}
}

func TestFFmpegInputFormat(t *testing.T) {
	if ffmpegInputFormat("YUYV") != "yuyv422" {
		t.Fatal("YUYV mapping failed")
	}
	if ffmpegInputFormat("MJPEG") != "mjpeg" {
		t.Fatal("MJPEG mapping failed")
	}
}

func TestParseFPSLabel(t *testing.T) {
	fps, err := parseFPSLabel("60 fps")
	if err != nil || fps != 60 {
		t.Fatalf("unexpected fps: %v %v", fps, err)
	}
}

func TestLoopbackFrameByteSize(t *testing.T) {
	if got := loopbackFrameByteSize(2560, 1440); got != 7372800 {
		t.Fatalf("unexpected frame size: %d", got)
	}
}