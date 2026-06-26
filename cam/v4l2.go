package cam

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// /dev/videoX
type V4L2_Device string

type Camera struct {
	Name         string // from v4l2-ctl --all -d /dev/videoX
	Device       V4L2_Device
	ColorFormats []V42L_ColorFormat
}

type V42L_ColorFormat struct {
	ShortName   string // YUYV; MJPEG
	LongName    string // YUYV 4:2:2; Motion-JPEG, compressed
	Resolutions []V42L_Resolution
}

type V42L_Resolution struct {
	Width      int
	Height     int
	FrameRates []float32
}

var (
	reListDevicesPath = regexp.MustCompile(`^(/dev/video\d+)$`)
	reCardType        = regexp.MustCompile(`(?m)^\s*Card type\s+:\s+(.+)$`)
	reColorFormat     = regexp.MustCompile(`^\[\d+\]: '([^']+)' \((.+)\)$`)
	reResolution      = regexp.MustCompile(`^Size: Discrete (\d+)x(\d+)$`)
	reFrameRate       = regexp.MustCompile(`^Interval: .+ \(([\d.]+) fps\)$`)
)

func v4l2Ctl(args ...string) (string, error) {
	cmd := exec.Command("v4l2-ctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf(
			"v4l2-ctl %s: %w: %s",
			strings.Join(args, " "),
			err,
			strings.TrimSpace(string(out)),
		)
	}
	return string(out), nil
}

func ListAllDevices() (map[string][]V4L2_Device, error) {
	out, err := v4l2Ctl("--list-devices")
	if err != nil {
		return nil, err
	}
	return parseListDevices(out), nil
}

func GetCamera(path string) (Camera, error) {
	return V4L2_Device(path).Details()
}

func (dev V4L2_Device) Details() (Camera, error) {
	device := string(dev)

	allOut, err := v4l2Ctl("--all", "-d", device)
	if err != nil {
		return Camera{}, err
	}

	formatsOut, err := v4l2Ctl("--list-formats-ext", "-d", device)
	if err != nil {
		return Camera{}, err
	}

	return Camera{
		Name:         parseCardType(allOut),
		Device:       dev,
		ColorFormats: parseFormatsExt(formatsOut),
	}, nil
}

func (cam Camera) Label() string {
	if cam.Name == "" {
		return string(cam.Device)
	}
	return fmt.Sprintf("%s (%s)", cam.Name, cam.Device)
}

func (cam Camera) LoopbackName() string {
	base := strings.TrimPrefix(string(cam.Device), "/dev/")
	sub := cam.SubName()
	if sub == "" {
		sub = base
	}
	return fmt.Sprintf("lo_%s: %s", base, sub)
}

func (cam Camera) SubName() string {
	name := cam.Name
	if i := strings.Index(name, ":"); i >= 0 {
		return strings.TrimSpace(name[:i])
	}
	return strings.TrimSpace(name)

}

func parseListDevices(output string) map[string][]V4L2_Device {
	ret := map[string][]V4L2_Device{}
	var current string

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if reListDevicesPath.MatchString(line) {
			if current != "" {
				ret[current] = append(ret[current], V4L2_Device(line))
			}
			continue
		}

		if strings.HasSuffix(line, ":") {
			current = strings.TrimSuffix(line, ":")
		}
	}

	return ret
}

func parseCardType(output string) string {
	if m := reCardType.FindStringSubmatch(output); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func parseFormatsExt(output string) []V42L_ColorFormat {
	var formats []V42L_ColorFormat
	var current *V42L_ColorFormat
	var resolution *V42L_Resolution

	flushResolution := func() {
		if current == nil || resolution == nil {
			return
		}
		current.Resolutions = append(current.Resolutions, *resolution)
		resolution = nil
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if m := reColorFormat.FindStringSubmatch(line); len(m) == 3 {
			flushResolution()
			if current != nil {
				formats = append(formats, *current)
			}
			current = &V42L_ColorFormat{
				ShortName: m[1],
				LongName:  m[2],
			}
			continue
		}

		if m := reResolution.FindStringSubmatch(line); len(m) == 3 {
			flushResolution()
			width, _ := strconv.Atoi(m[1])
			height, _ := strconv.Atoi(m[2])
			resolution = &V42L_Resolution{
				Width:  width,
				Height: height,
			}
			continue
		}

		if m := reFrameRate.FindStringSubmatch(line); len(m) == 2 && resolution != nil {
			fps, err := strconv.ParseFloat(m[1], 32)
			if err == nil {
				resolution.FrameRates = append(resolution.FrameRates, float32(fps))
			}
		}
	}

	flushResolution()
	if current != nil {
		formats = append(formats, *current)
	}

	return formats
}
