package cam

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"
)

type ui struct {
	sourceCombo      *qt6.QComboBox
	destCombo        *qt6.QComboBox
	colorFormatCombo *qt6.QComboBox
	resolutionCombo  *qt6.QComboBox
	framerateCombo   *qt6.QComboBox
	previewLabel     *qt6.QLabel

	cameras []Camera
	targets []Camera
	loopback *Loopback
	shuttingDown bool
}

func RunGUI(args []string) {
	qt6.QCoreApplication_SetApplicationName("wl.float")
	qt6.QGuiApplication_SetDesktopFileName("wl.float")
	qt6.NewQApplication(args)
	defer qt6.QApplication_Exec()

	icon := cameraIcon()
	qt6.QGuiApplication_SetWindowIcon(icon)

	win := qt6.NewQWidget(nil)
	win.SetWindowTitle("Cam Config")
	win.SetWindowIcon(icon)
	win.SetMinimumSize2(520, 420)

	u := &ui{}

	// === Top row: Source → Destination ===
	u.sourceCombo = qt6.NewQComboBox2()
	u.sourceCombo.SetMinimumWidth(200)
	u.sourceCombo.OnCurrentIndexChanged(func(index int) {
		u.refreshSourcesIfNeeded()
		u.onSourceChanged(index)
		u.tryAutoUpdatePipeline()
	})

	arrowLabel := qt6.NewQLabel2("→")
	arrowLabel.SetAlignment(qt6.Qt__AlignCenter)
	arrowLabel.SetStyleSheet("font-size: 18pt; font-weight: bold;")

	u.destCombo = qt6.NewQComboBox2()
	u.destCombo.SetMinimumWidth(200)
	u.destCombo.OnCurrentIndexChanged(func(index int) {
		u.refreshTargetsIfNeeded()
		u.tryAutoUpdatePipeline()
	})

	topRow := qt6.NewQHBoxLayout2()
	topRow.AddWidget2(u.sourceCombo.QWidget, 1)
	topRow.AddWidget(arrowLabel.QWidget)
	topRow.AddWidget2(u.destCombo.QWidget, 1)

	// === Middle row: Format / Resolution / FPS ===
	u.colorFormatCombo = qt6.NewQComboBox2()
	u.colorFormatCombo.OnCurrentIndexChanged(func(index int) {
		u.onColorFormatChanged(index)
		u.updateColorFormatTooltip()
		u.tryAutoUpdatePipeline()
	})

	u.resolutionCombo = qt6.NewQComboBox2()
	u.resolutionCombo.OnCurrentIndexChanged(func(index int) {
		u.onResolutionChanged(index)
		u.tryAutoUpdatePipeline()
	})

	u.framerateCombo = qt6.NewQComboBox2()
	u.framerateCombo.OnCurrentIndexChanged(func(index int) {
		u.onFramerateChanged(index)
		u.tryAutoUpdatePipeline()
	})

	formatRow := qt6.NewQHBoxLayout2()
	formatRow.AddWidget2(u.colorFormatCombo.QWidget, 1)
	formatRow.AddWidget2(u.resolutionCombo.QWidget, 1)
	formatRow.AddWidget2(u.framerateCombo.QWidget, 1)

	// === Preview ===
	u.previewLabel = qt6.NewQLabel2()
	u.previewLabel.SetMinimumSize2(320, 240)
	u.previewLabel.SetAlignment(qt6.Qt__AlignCenter)
	u.previewLabel.SetStyleSheet("background-color: #222; color: #888; border: 1px solid #444;")
	u.previewLabel.SetText("Preview will appear here when streaming")

	// Main layout
	root := qt6.NewQVBoxLayout(win)
	root.SetContentsMargins(12, 12, 12, 12)
	root.SetSpacing(10)
	root.AddLayout(topRow.QLayout)
	root.AddLayout(formatRow.QLayout)
	root.AddWidget2(u.previewLabel.QWidget, 1)

	u.refreshSources()
	u.refreshTargets()

	win.OnCloseEvent(func(_ func(_ *qt6.QCloseEvent), _ *qt6.QCloseEvent) {
		u.shuttingDown = true
		u.shutdownLoopback()
		qt6.QCoreApplication_Quit()
	})
	win.Show()
}

// Refresh when user opens/interacts with dropdowns
func (u *ui) refreshSourcesIfNeeded() {
	go func() {
		cams, _ := enumerateCameras()
		mainthread.Start(func() {
			u.cameras = cams
			labels := make([]string, len(cams))
			for i, c := range cams {
				labels[i] = c.Label()
			}
			if len(labels) == 0 {
				labels = []string{"(no cameras)"}
			}
			u.fillCombo(u.sourceCombo, labels)
		})
	}()
}

func (u *ui) refreshTargetsIfNeeded() {
	go func() {
		lbs, _ := ListLoopbackDevices()
		mainthread.Start(func() {
			u.targets = lbs
			labels := make([]string, len(lbs))
			for i, lb := range lbs {
				labels[i] = lb.Label()
			}
			if len(labels) == 0 {
				labels = []string{"(no loopback devices found)"}
			}
			u.fillCombo(u.destCombo, labels)
		})
	}()
}

func (u *ui) refreshSources() {
	go func() {
		cams, _ := enumerateCameras()
		mainthread.Start(func() {
			u.cameras = cams
			labels := make([]string, len(cams))
			for i, c := range cams {
				labels[i] = c.Label()
			}
			if len(labels) == 0 {
				labels = []string{"(no cameras)"}
			}
			u.fillCombo(u.sourceCombo, labels)
		})
	}()
}

func (u *ui) refreshTargets() {
	go func() {
		lbs, _ := ListLoopbackDevices()
		mainthread.Start(func() {
			u.targets = lbs
			labels := make([]string, len(lbs))
			for i, lb := range lbs {
				labels[i] = lb.Label()
			}
			if len(labels) == 0 {
				labels = []string{"(no loopback devices found)"}
			}
			u.fillCombo(u.destCombo, labels)
		})
	}()
}

func (u *ui) tryAutoUpdatePipeline() {
	if u.shuttingDown {
		return
	}

	sIdx := u.sourceCombo.CurrentIndex()
	dIdx := u.destCombo.CurrentIndex()

	if sIdx < 0 || sIdx >= len(u.cameras) || dIdx < 0 || dIdx >= len(u.targets) {
		return
	}

	cfg := u.buildCurrentConfig()
	if cfg.Source == "" || cfg.Target == "" {
		return
	}

	if u.loopback == nil {
		go func() {
			lb, err := cfg.Start()
			mainthread.Start(func() {
				if err != nil || u.shuttingDown {
					return
				}
				u.loopback = lb
				u.startPreview(lb.OutputDevice())
			})
		}()
	} else {
		go func() {
			_ = u.loopback.Reset()
		}()
	}
}

func (u *ui) buildCurrentConfig() LoopbackConfig {
	sIdx := u.sourceCombo.CurrentIndex()
	dIdx := u.destCombo.CurrentIndex()
	fIdx := u.colorFormatCombo.CurrentIndex()
	rIdx := u.resolutionCombo.CurrentIndex()
	fpsIdx := u.framerateCombo.CurrentIndex()

	if sIdx < 0 || sIdx >= len(u.cameras) ||
		dIdx < 0 || dIdx >= len(u.targets) ||
		fIdx < 0 || fIdx >= len(u.cameras[sIdx].ColorFormats) {
		return LoopbackConfig{}
	}

	resolutions := sortedResolutions(u.cameras[sIdx].ColorFormats[fIdx].Resolutions)
	if rIdx < 0 || rIdx >= len(resolutions) {
		return LoopbackConfig{}
	}

	fpsText := u.framerateCombo.ItemText(fpsIdx)
	fps, _ := parseFPSLabel(fpsText)

	res := resolutions[rIdx]
	return LoopbackConfig{
		Source:    u.cameras[sIdx].Device,
		Target:    u.targets[dIdx].Device,
		Format:    u.cameras[sIdx].ColorFormats[fIdx].ShortName,
		Width:     res.Width,
		Height:    res.Height,
		FrameRate: fps,
	}
}

func (u *ui) onSourceChanged(index int) {
	if index < 0 || index >= len(u.cameras) {
		u.fillColorFormats(nil)
		u.fillCombo(u.resolutionCombo, nil)
		u.fillCombo(u.framerateCombo, nil)
		return
	}

	u.fillColorFormats(u.cameras[index].ColorFormats)
	u.onColorFormatChanged(0)
	u.updateColorFormatTooltip()
}

func (u *ui) onColorFormatChanged(index int) {
	sIdx := u.sourceCombo.CurrentIndex()
	if sIdx < 0 || sIdx >= len(u.cameras) || index < 0 || index >= len(u.cameras[sIdx].ColorFormats) {
		u.fillCombo(u.resolutionCombo, nil)
		u.fillCombo(u.framerateCombo, nil)
		return
	}

	prevRes := u.resolutionCombo.CurrentText()
	prevFps := u.framerateCombo.CurrentText()

	resolutions := sortedResolutions(u.cameras[sIdx].ColorFormats[index].Resolutions)
	resLabels := resolutionLabels(resolutions)
	resIdx := findResolutionLabelIndex(resolutions, prevRes)
	u.fillComboSelect(u.resolutionCombo, resLabels, resIdx)

	u.populateFramerates(resIdx, prevFps)
}

func (u *ui) onResolutionChanged(index int) {
	prevFps := u.framerateCombo.CurrentText()
	u.populateFramerates(index, prevFps)
}

func (u *ui) onFramerateChanged(index int) {
}

func (u *ui) populateFramerates(resIdx int, prefer string) {
	sIdx := u.sourceCombo.CurrentIndex()
	fIdx := u.colorFormatCombo.CurrentIndex()

	if sIdx < 0 || sIdx >= len(u.cameras) || fIdx < 0 || fIdx >= len(u.cameras[sIdx].ColorFormats) {
		u.fillCombo(u.framerateCombo, nil)
		return
	}

	resolutions := sortedResolutions(u.cameras[sIdx].ColorFormats[fIdx].Resolutions)
	if resIdx < 0 || resIdx >= len(resolutions) {
		u.fillCombo(u.framerateCombo, nil)
		return
	}

	rates := resolutions[resIdx].FrameRates
	labels := fpsLabels(rates)
	idx := findFPSLabelIndex(rates, prefer)
	u.fillComboSelect(u.framerateCombo, labels, idx)
}

func (u *ui) shutdownLoopback() {
	if u.loopback != nil {
		_ = u.loopback.Stop()
		u.loopback = nil
	}
}

func (u *ui) startPreview(device V4L2_Device) {
	if device == "" {
		return
	}

	go func() {
		for {
			if u.shuttingDown || u.loopback == nil {
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
			cmd := exec.CommandContext(ctx, "ffmpeg",
				"-hide_banner", "-loglevel", "error",
				"-f", "v4l2", "-i", string(device),
				"-vf", "scale=320:-1",
				"-frames:v", "1",
				"-f", "image2pipe", "-")

			out, err := cmd.Output()
			cancel()

			if err == nil && len(out) > 0 {
				img, _, decodeErr := image.Decode(bytes.NewReader(out))
				if decodeErr == nil && img != nil {
					mainthread.Start(func() {
						qimg := qt6.QImage_FromImage(img)
						pix := qt6.QPixmap_FromImage(qimg)
						u.previewLabel.SetPixmap(pix)
					})
				}
			}

			time.Sleep(150 * time.Millisecond)
		}
	}()
}

func (u *ui) fillColorFormats(formats []V42L_ColorFormat) {
	u.colorFormatCombo.BlockSignals(true)
	defer u.colorFormatCombo.BlockSignals(false)

	u.colorFormatCombo.Clear()
	if len(formats) == 0 {
		u.colorFormatCombo.AddItem("(none)")
		u.colorFormatCombo.SetEnabled(false)
		return
	}

	u.colorFormatCombo.SetEnabled(true)
	for _, f := range formats {
		u.colorFormatCombo.AddItem(f.ShortName)
	}
}

func (u *ui) updateColorFormatTooltip() {
	sIdx := u.sourceCombo.CurrentIndex()
	fIdx := u.colorFormatCombo.CurrentIndex()
	if sIdx < 0 || sIdx >= len(u.cameras) || fIdx < 0 || fIdx >= len(u.cameras[sIdx].ColorFormats) {
		u.colorFormatCombo.SetToolTip("")
		return
	}
	u.colorFormatCombo.SetToolTip(u.cameras[sIdx].ColorFormats[fIdx].LongName)
}

func (u *ui) fillCombo(combo *qt6.QComboBox, items []string) {
	combo.BlockSignals(true)
	defer combo.BlockSignals(false)
	combo.Clear()
	if len(items) == 0 {
		combo.AddItem("(none)")
		combo.SetEnabled(false)
		return
	}
	combo.SetEnabled(true)
	combo.AddItems(items)
}

func (u *ui) fillComboSelect(combo *qt6.QComboBox, items []string, index int) {
	combo.BlockSignals(true)
	defer combo.BlockSignals(false)
	combo.Clear()
	if len(items) == 0 {
		combo.AddItem("(none)")
		combo.SetEnabled(false)
		return
	}
	combo.SetEnabled(true)
	combo.AddItems(items)
	if index >= 0 && index < len(items) {
		combo.SetCurrentIndex(index)
	}
}

func resolutionLabels(resolutions []V42L_Resolution) []string {
	labels := make([]string, len(resolutions))
	for i, res := range resolutions {
		labels[i] = resolutionLabel(res)
	}
	return labels
}

func fpsLabels(rates []float32) []string {
	labels := make([]string, len(rates))
	for i, fps := range rates {
		labels[i] = fpsLabel(fps)
	}
	return labels
}

func findResolutionLabelIndex(resolutions []V42L_Resolution, label string) int {
	if label == "" || label == "(none)" {
		return 0
	}
	for i, res := range resolutions {
		if resolutionLabel(res) == label {
			return i
		}
	}
	return 0
}

func findFPSLabelIndex(rates []float32, label string) int {
	if label == "" || label == "(none)" {
		return 0
	}
	for i, fps := range rates {
		if fpsLabel(fps) == label {
			return i
		}
	}
	return 0
}

func sortedResolutions(res []V42L_Resolution) []V42L_Resolution {
	out := slices.Clone(res)
	slices.SortFunc(out, func(a, b V42L_Resolution) int {
		pa := a.Width * a.Height
		pb := b.Width * b.Height
		if pa > pb {
			return -1
		}
		if pa < pb {
			return 1
		}
		return 0
	})
	return out
}

func resolutionLabel(res V42L_Resolution) string {
	return fmt.Sprintf("%d\u00d7%d", res.Width, res.Height)
}

func cameraIcon() *qt6.QIcon {
	for _, name := range []string{"camera-video", "camera-web", "camera-photo"} {
		icon := qt6.QIcon_FromTheme(name)
		if !icon.IsNull() {
			return icon
		}
	}
	return qt6.NewQIcon()
}

func fpsLabel(fps float32) string {
	if fps == float32(int(fps)) {
		return strconv.Itoa(int(fps)) + " fps"
	}
	return strconv.FormatFloat(float64(fps), 'g', -1, 32) + " fps"
}
