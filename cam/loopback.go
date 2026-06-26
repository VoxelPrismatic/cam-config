package cam

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type LoopbackConfig struct {
	Source    V4L2_Device
	Target    V4L2_Device // pre-existing v4l2loopback device (e.g. /dev/video10)
	Format    string
	Width     int
	Height    int
	FrameRate float32
}

type Loopback struct {
	cfg          LoopbackConfig
	outputDevice V4L2_Device
	ffmpeg       *exec.Cmd
	stop         chan struct{}
	stopOnce     sync.Once
	stopErr      error
	mu           sync.Mutex
}

func (cfg LoopbackConfig) Start() (*Loopback, error) {
	if cfg.Source == "" {
		return nil, fmt.Errorf("no source device")
	}
	if cfg.Target == "" {
		return nil, fmt.Errorf("no target loopback device (e.g. /dev/video10)")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("invalid resolution %dx%d", cfg.Width, cfg.Height)
	}
	if cfg.FrameRate <= 0 {
		return nil, fmt.Errorf("invalid frame rate")
	}
	if cfg.Format == "" {
		return nil, fmt.Errorf("no pixel format")
	}

	lb := &Loopback{
		cfg:          cfg,
		outputDevice: cfg.Target,
		stop:         make(chan struct{}),
	}

	if err := lb.startPipeline(); err != nil {
		return nil, err
	}

	logLoopbackReady(lb.outputDevice)
	return lb, nil
}

func (lb *Loopback) Stop() error {
	if lb == nil {
		return nil
	}

	lb.stopOnce.Do(func() {
		close(lb.stop)

		lb.mu.Lock()
		cmd := lb.ffmpeg
		lb.ffmpeg = nil
		lb.mu.Unlock()

		waitProcessExit(cmd, 3*time.Second)
	})

	return lb.stopErr
}

func (lb *Loopback) Reset() error {
	if lb == nil {
		return fmt.Errorf("loopback not running")
	}

	lb.mu.Lock()
	defer lb.mu.Unlock()

	select {
	case <-lb.stop:
		return fmt.Errorf("loopback stopped")
	default:
	}

	return lb.restartPipelineLocked("manual reset")
}

func (lb *Loopback) OutputDevice() V4L2_Device {
	if lb == nil {
		return ""
	}
	return lb.outputDevice
}

func (lb *Loopback) startPipeline() error {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.restartPipelineLocked("")
}

func (lb *Loopback) restartPipelineLocked(reason string) error {
	if reason != "" {
		logRestart(reason)
	}

	old := lb.ffmpeg
	lb.ffmpeg = nil
	if old != nil {
		lb.mu.Unlock()
		waitProcessExit(old, 3*time.Second)
		lb.mu.Lock()
	}

	if err := configureCapture(lb.cfg); err != nil {
		return fmt.Errorf("configure capture device: %w", err)
	}

	if err := configureLoopbackDevice(lb.outputDevice, lb.cfg); err != nil {
		return fmt.Errorf("configure loopback device: %w", err)
	}

	cmd, err := startFFmpeg(lb.cfg, lb.outputDevice)
	if err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	lb.ffmpeg = cmd
	go lb.monitorFFmpeg(cmd)
	return nil
}

func (lb *Loopback) monitorFFmpeg(cmd *exec.Cmd) {
	_ = cmd.Wait()

	lb.mu.Lock()
	defer lb.mu.Unlock()

	if lb.ffmpeg == cmd {
		lb.ffmpeg = nil
	}
}

func waitProcessExit(cmd *exec.Cmd, timeout time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	pid := cmd.Process.Pid
	_ = cmd.Process.Signal(syscall.SIGINT)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	_ = syscall.Kill(pid, syscall.SIGKILL)
	for time.Now().Before(deadline.Add(2 * time.Second)) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}
}

// ConfigureCapture applies the selected format and frame rate to the source device.
func ConfigureCapture(cfg LoopbackConfig) error {
	return configureCapture(cfg)
}

func configureCapture(cfg LoopbackConfig) error {
	fps := int(cfg.FrameRate + 0.5)
	_, err := v4l2Ctl(
		"-d", string(cfg.Source),
		fmt.Sprintf("--set-fmt-video=width=%d,height=%d,pixelformat=%s", cfg.Width, cfg.Height, v4l2PixelFormat(cfg.Format)),
	)
	if err != nil {
		return err
	}
	_, err = v4l2Ctl("-d", string(cfg.Source), "-p", strconv.Itoa(fps))
	return err
}

func configureLoopbackDevice(device V4L2_Device, cfg LoopbackConfig) error {
	_, err := v4l2Ctl(
		"-d", string(device),
		fmt.Sprintf("--set-fmt-video-out=width=%d,height=%d,pixelformat=YUYV", cfg.Width, cfg.Height),
	)
	if err != nil {
		return err
	}

	// Repeat frames at the target rate so PipeWire/Chromium consumers see a live stream.
	_, _ = v4l2Ctl("-d", string(device), "-c", "sustain_framerate=1")
	return nil
}

func logLoopbackReady(device V4L2_Device) {
	fmt.Fprintf(os.Stderr, "[cam-config] streaming to loopback at %s\n", device)
	fmt.Fprintf(os.Stderr, "[cam-config] flatpak browsers reach cameras via PipeWire; if video stays blank, try a native browser or grant device access with flatpak override --user --device=all <app-id>\n")
}

func startFFmpeg(cfg LoopbackConfig, output V4L2_Device) (*exec.Cmd, error) {
	fps := strconv.FormatFloat(float64(cfg.FrameRate), 'g', -1, 32)
	size := fmt.Sprintf("%dx%d", cfg.Width, cfg.Height)

	// Read in real time and emit a steady YUYV stream. Browsers via PipeWire need
	// continuous frames with timestamps; burst-then-stall causes ready-timeout errors.
	args := []string{
		"-hide_banner",
		"-nostats",
		"-loglevel", "warning",
		"-thread_queue_size", "512",
		"-re",
		"-fflags", "+genpts",
		"-use_wallclock_as_timestamps", "1",
		"-f", "v4l2",
		"-input_format", ffmpegInputFormat(cfg.Format),
		"-video_size", size,
		"-framerate", fps,
		"-i", string(cfg.Source),
		"-pix_fmt", "yuyv422",
		"-r", fps,
		"-fps_mode", "cfr",
		"-f", "v4l2",
		string(output),
	}

	cmd := exec.Command("ffmpeg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := attachFFmpegLogs(cmd); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

func v4l2PixelFormat(fourcc string) string {
	switch strings.ToUpper(fourcc) {
	case "MJPEG":
		return "MJPG"
	default:
		return strings.ToUpper(fourcc)
	}
}

func ffmpegInputFormat(fourcc string) string {
	switch strings.ToUpper(fourcc) {
	case "YUYV":
		return "yuyv422"
	case "MJPEG", "MJPG":
		return "mjpeg"
	case "NV12":
		return "nv12"
	case "RGB3":
		return "rgb24"
	case "BGR3":
		return "bgr24"
	case "UYVY":
		return "uyvy422"
	default:
		return strings.ToLower(fourcc)
	}
}

func parseFPSLabel(label string) (float32, error) {
	label = strings.TrimSpace(label)
	label = strings.TrimSuffix(label, " fps")
	f, err := strconv.ParseFloat(label, 32)
	if err != nil {
		return 0, err
	}
	return float32(f), nil
}
