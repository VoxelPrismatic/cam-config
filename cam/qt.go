package cam

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"
)

type ui struct {
	targetCombo *qt6.QComboBox
	loopbackBtn *qt6.QPushButton
	cameraCombo *qt6.QComboBox
	refreshBtn  *qt6.QPushButton
	colorFormatCombo *qt6.QComboBox
	resolutionCombo  *qt6.QComboBox
	framerateCombo   *qt6.QComboBox

	cameras []Camera
	targets []Camera
	loopback *Loopback
	loopbackActive bool
	shuttingDown bool
	playIcon *qt6.QIcon
	stopIcon *qt6.QIcon
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
	win.SetMinimumSize2(400, 140)

	style := qt6.QApplication_Style()
	u := &ui{
		playIcon: style.StandardIcon(qt6.QStyle__SP_MediaPlay, nil, nil),
		stopIcon: style.StandardIcon(qt6.QStyle__SP_MediaStop, nil, nil),
	}

	u.targetCombo = qt6.NewQComboBox2()
	u.targetCombo.SetMinimumWidth(200)

	u.loopbackBtn = qt6.NewQPushButton2()
	u.loopbackBtn.SetIcon(u.playIcon)
	u.loopbackBtn.OnClicked(u.toggleLoopback)

	u.cameraCombo = qt6.NewQComboBox2()
	u.cameraCombo.OnCurrentIndexChanged(u.onCameraChanged)

	u.refreshBtn = qt6.NewQPushButton2()
	u.refreshBtn.SetIcon(style.StandardIcon(qt6.QStyle__SP_BrowserReload, nil, nil))
	u.refreshBtn.OnClicked(func() {
		u.refreshCameras()
		u.refreshTargets()
	})

	u.colorFormatCombo = qt6.NewQComboBox2()
	u.colorFormatCombo.OnCurrentIndexChanged(func(index int) {
		u.onColorFormatChanged(index)
		u.updateColorFormatTooltip()
	})

	u.resolutionCombo = qt6.NewQComboBox2()
	u.resolutionCombo.OnCurrentIndexChanged(u.onResolutionChanged)

	u.framerateCombo = qt6.NewQComboBox2()
	u.framerateCombo.OnCurrentIndexChanged(u.onFramerateChanged)

	targetRow := qt6.NewQHBoxLayout2()
	targetRow.AddWidget2(u.targetCombo.QWidget, 1)
	targetRow.AddWidget(u.loopbackBtn.QWidget)

	cameraRow := qt6.NewQHBoxLayout2()
	cameraRow.AddWidget2(u.cameraCombo.QWidget, 1)
	cameraRow.AddWidget(u.refreshBtn.QWidget)

	formatRow := qt6.NewQHBoxLayout2()
	formatRow.AddWidget2(u.colorFormatCombo.QWidget, 1)
	formatRow.AddWidget2(u.resolutionCombo.QWidget, 1)
	formatRow.AddWidget2(u.framerateCombo.QWidget, 1)

	root := qt6.NewQVBoxLayout(win)
	root.SetContentsMargins(12, 12, 12, 12)
	root.SetSpacing(8)
	root.AddLayout(targetRow.QLayout)
	root.AddLayout(cameraRow.QLayout)
	root.AddLayout(formatRow.QLayout)

	u.refreshCameras()
	u.refreshTargets()
	win.OnCloseEvent(func(_ func(_ *qt6.QCloseEvent), _ *qt6.QCloseEvent) {
		u.shuttingDown = true
		u.shutdownLoopback()
		qt6.QCoreApplication_Quit()
	})
	win.Show()
}

func (u *ui) setLoopbackActive(active bool) {
	u.loopbackActive = active

	u.targetCombo.SetEnabled(!active)
	u.cameraCombo.SetEnabled(!active)
	u.refreshBtn.SetEnabled(!active)
	u.colorFormatCombo.SetEnabled(!active)
	u.resolutionCombo.SetEnabled(!active)
	u.framerateCombo.SetEnabled(!active)

	if active {
		u.loopbackBtn.SetIcon(u.stopIcon)
	} else {
		u.loopbackBtn.SetIcon(u.playIcon)
	}
}

func (u *ui) toggleLoopback() {
	if u.loopbackActive {
		u.stopLoopback()
		return
	}

	cfg, err := u.loopbackConfig()
	if err != nil {
		return
	}

	u.loopbackBtn.SetEnabled(false)
	go func() {
		lb, err := cfg.Start()
		mainthread.Start(func() {
			if u.shuttingDown {
				if lb != nil {
					_ = lb.Stop()
				}
				return
			}
			u.loopbackBtn.SetEnabled(true)
			if err != nil {
				return
			}
			u.loopback = lb
			u.setLoopbackActive(true)
		})
	}()
}

func (u *ui) stopLoopback() {
	lb := u.loopback
	u.loopback = nil
	u.loopbackBtn.SetEnabled(false)

	if lb != nil {
		_ = lb.Stop()
	}

	u.loopbackBtn.SetEnabled(true)
	u.setLoopbackActive(false)
}

func (u *ui) shutdownLoopback() {
	lb := u.loopback
	u.loopback = nil
	if lb != nil {
		_ = lb.Stop()
	}
	u.setLoopbackActive(false)
}

func (u *ui) loopbackConfig() (LoopbackConfig, error) {
	cfg, err := u.captureConfig()
	if err != nil {
		return LoopbackConfig{}, err
	}

	tIdx := u.targetCombo.CurrentIndex()
	if tIdx < 0 || tIdx >= len(u.targets) {
		return LoopbackConfig{}, fmt.Errorf("no target loopback selected")
	}

	cfg.Target = u.targets[tIdx].Device
	return cfg, nil
}

func (u *ui) refreshCameras() {
	u.refreshBtn.SetEnabled(false)

	go func() {
		cameras, err := enumerateCameras()
		mainthread.Start(func() {
			if !u.loopbackActive {
				u.refreshBtn.SetEnabled(true)
			}
			if err != nil {
				u.cameras = nil
			u.fillCombo(u.cameraCombo, []string{fmt.Sprintf("(error: %v)", err)})
			u.fillColorFormats(nil)
			u.fillCombo(u.resolutionCombo, nil)
			u.fillCombo(u.framerateCombo, nil)
			return
			}

			u.cameras = cameras
			labels := make([]string, len(cameras))
			for i, cam := range cameras {
				labels[i] = cam.Label()
			}
			if len(labels) == 0 {
				u.fillCombo(u.cameraCombo, []string{"(no cameras)"})
				u.fillColorFormats(nil)
				u.fillCombo(u.resolutionCombo, nil)
				u.fillCombo(u.framerateCombo, nil)
				return
			}

			u.fillCombo(u.cameraCombo, labels)
			u.onCameraChanged(0)
		})
	}()
}

func (u *ui) refreshTargets() {
	go func() {
		loopbacks, err := ListLoopbackDevices()
		mainthread.Start(func() {
			if err != nil || len(loopbacks) == 0 {
				u.targets = nil
				u.fillCombo(u.targetCombo, []string{"(no loopback devices found)"})
				u.targetCombo.SetEnabled(false)
				return
			}

			u.targets = loopbacks
			labels := make([]string, len(loopbacks))
			for i, lb := range loopbacks {
				labels[i] = lb.Label()
			}
			u.fillCombo(u.targetCombo, labels)
			u.targetCombo.SetEnabled(!u.loopbackActive)
		})
	}()
}

func enumerateCameras() ([]Camera, error) {
	groups, err := ListAllDevices()
	if err != nil {
		return nil, err
	}

	var cameras []Camera
	for _, devices := range groups {
		for _, dev := range devices {
			if IsV4L2Loopback(dev) {
				continue
			}
			cam, err := GetCamera(string(dev))
			if err != nil || len(cam.ColorFormats) == 0 {
				continue
			}
			cameras = append(cameras, cam)
		}
	}
	return cameras, nil
}

func (u *ui) onCameraChanged(index int) {
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
	camIdx := u.cameraCombo.CurrentIndex()
	if camIdx < 0 || camIdx >= len(u.cameras) || index < 0 || index >= len(u.cameras[camIdx].ColorFormats) {
		u.fillCombo(u.resolutionCombo, nil)
		u.fillCombo(u.framerateCombo, nil)
		return
	}

	prevResolution := u.resolutionCombo.CurrentText()
	prevFramerate := u.framerateCombo.CurrentText()

	resolutions := sortedResolutions(u.cameras[camIdx].ColorFormats[index].Resolutions)
	resLabels := resolutionLabels(resolutions)
	resIdx := findResolutionLabelIndex(resolutions, prevResolution)
	u.fillComboSelect(u.resolutionCombo, resLabels, resIdx)

	u.populateFramerates(resIdx, prevFramerate)
	u.applyCaptureSettings()
}

func (u *ui) onResolutionChanged(index int) {
	prevFramerate := u.framerateCombo.CurrentText()
	u.populateFramerates(index, prevFramerate)
	u.applyCaptureSettings()
}

func (u *ui) onFramerateChanged(_ int) {
	u.applyCaptureSettings()
}

func (u *ui) populateFramerates(resIdx int, preferFramerate string) {
	camIdx := u.cameraCombo.CurrentIndex()
	fmtIdx := u.colorFormatCombo.CurrentIndex()
	resolutions := []V42L_Resolution{}
	if camIdx >= 0 && camIdx < len(u.cameras) &&
		fmtIdx >= 0 && fmtIdx < len(u.cameras[camIdx].ColorFormats) {
		resolutions = sortedResolutions(u.cameras[camIdx].ColorFormats[fmtIdx].Resolutions)
	}
	if resIdx < 0 || resIdx >= len(resolutions) {
		u.fillCombo(u.framerateCombo, nil)
		return
	}

	rates := resolutions[resIdx].FrameRates
	fpsLabels := fpsLabels(rates)
	fpsIdx := findFPSLabelIndex(rates, preferFramerate)
	u.fillComboSelect(u.framerateCombo, fpsLabels, fpsIdx)
}

func (u *ui) applyCaptureSettings() {
	if u.loopbackActive {
		return
	}

	cfg, err := u.captureConfig()
	if err != nil {
		return
	}

	_ = ConfigureCapture(cfg)
}

func (u *ui) captureConfig() (LoopbackConfig, error) {
	camIdx := u.cameraCombo.CurrentIndex()
	fmtIdx := u.colorFormatCombo.CurrentIndex()
	resIdx := u.resolutionCombo.CurrentIndex()
	fpsIdx := u.framerateCombo.CurrentIndex()

	if camIdx < 0 || camIdx >= len(u.cameras) {
		return LoopbackConfig{}, fmt.Errorf("no camera selected")
	}
	if fmtIdx < 0 || fmtIdx >= len(u.cameras[camIdx].ColorFormats) {
		return LoopbackConfig{}, fmt.Errorf("no color format selected")
	}

	resolutions := sortedResolutions(u.cameras[camIdx].ColorFormats[fmtIdx].Resolutions)
	if resIdx < 0 || resIdx >= len(resolutions) {
		return LoopbackConfig{}, fmt.Errorf("no resolution selected")
	}

	fpsText := u.framerateCombo.ItemText(fpsIdx)
	if fpsText == "" || fpsText == "(none)" {
		return LoopbackConfig{}, fmt.Errorf("no frame rate selected")
	}
	fps, err := parseFPSLabel(fpsText)
	if err != nil {
		return LoopbackConfig{}, fmt.Errorf("invalid frame rate: %w", err)
	}

	res := resolutions[resIdx]
	return LoopbackConfig{
		Source:    u.cameras[camIdx].Device,
		Format:    u.cameras[camIdx].ColorFormats[fmtIdx].ShortName,
		Width:     res.Width,
		Height:    res.Height,
		FrameRate: fps,
	}, nil
}

func (u *ui) fillColorFormats(formats []V42L_ColorFormat) {
	u.colorFormatCombo.BlockSignals(true)
	defer u.colorFormatCombo.BlockSignals(false)

	u.colorFormatCombo.Clear()
	if len(formats) == 0 {
		u.colorFormatCombo.AddItem("(none)")
		u.colorFormatCombo.SetToolTip("")
		u.colorFormatCombo.SetEnabled(false)
		return
	}

	u.colorFormatCombo.SetEnabled(!u.loopbackActive)
	for i, format := range formats {
		u.colorFormatCombo.AddItem(format.ShortName)
		u.colorFormatCombo.SetItemData2(i, qt6.NewQVariant11(format.LongName), int(qt6.ToolTipRole))
	}
}

func (u *ui) updateColorFormatTooltip() {
	camIdx := u.cameraCombo.CurrentIndex()
	fmtIdx := u.colorFormatCombo.CurrentIndex()
	if camIdx < 0 || camIdx >= len(u.cameras) ||
		fmtIdx < 0 || fmtIdx >= len(u.cameras[camIdx].ColorFormats) {
		u.colorFormatCombo.SetToolTip("")
		return
	}
	u.colorFormatCombo.SetToolTip(u.cameras[camIdx].ColorFormats[fmtIdx].LongName)
}

func (u *ui) fillCombo(combo *qt6.QComboBox, items []string) {
	u.fillComboSelect(combo, items, 0)
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

	combo.SetEnabled(!u.loopbackActive)
	combo.AddItems(items)

	if index < 0 || index >= len(items) {
		index = 0
	}
	combo.SetCurrentIndex(index)
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
