package cam

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"
)

type ui struct {
	sourceCombo      *qt6.QComboBox
	destCombo        *qt6.QComboBox
	colorFormatCombo *qt6.QComboBox
	resolutionCombo  *qt6.QComboBox
	framerateCombo   *qt6.QComboBox

	cameras  []Camera
	targets  []Camera
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
	win.SetFixedSize2(480, 160)

	u := &ui{}

	// === Source row ===
	sourceIcon := rowIconLabel(sourceRowIcon())
	u.sourceCombo = qt6.NewQComboBox2()
	u.sourceCombo.OnCurrentIndexChanged(func(index int) {
		u.onSourceChanged(index)
		u.tryAutoUpdatePipeline()
	})

	sourceRow := qt6.NewQHBoxLayout2()
	sourceRow.AddWidget(sourceIcon.QWidget)
	sourceRow.AddWidget2(u.sourceCombo.QWidget, 1)

	// === Destination row ===
	destIcon := rowIconLabel(destRowIcon())
	u.destCombo = qt6.NewQComboBox2()
	u.destCombo.OnCurrentIndexChanged(func(index int) {
		u.tryAutoUpdatePipeline()
	})

	destRow := qt6.NewQHBoxLayout2()
	destRow.AddWidget(destIcon.QWidget)
	destRow.AddWidget2(u.destCombo.QWidget, 1)

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

	configureIcon := rowIconLabel(configureRowIcon())

	refreshBtn := qt6.NewQToolButton2()
	refreshBtn.SetIcon(refreshIcon())
	refreshBtn.SetToolTip("Refresh devices and restart stream")
	refreshBtn.SetAutoRaise(true)
	refreshBtn.OnClicked(func() {
		u.refreshDevices()
		u.resetPipeline()
	})

	formatRow := qt6.NewQHBoxLayout2()
	formatRow.AddWidget(configureIcon.QWidget)
	formatRow.AddWidget2(u.colorFormatCombo.QWidget, 1)
	formatRow.AddWidget2(u.resolutionCombo.QWidget, 1)
	formatRow.AddWidget2(u.framerateCombo.QWidget, 1)
	formatRow.AddWidget(refreshBtn.QWidget)

	root := qt6.NewQVBoxLayout(win)
	root.SetContentsMargins(12, 12, 12, 12)
	root.SetSpacing(10)
	root.AddLayout(sourceRow.QLayout)
	root.AddLayout(destRow.QLayout)
	root.AddLayout(formatRow.QLayout)

	u.refreshDevices()

	win.OnCloseEvent(func(_ func(_ *qt6.QCloseEvent), _ *qt6.QCloseEvent) {
		u.shuttingDown = true
		u.shutdownLoopback()
		qt6.QCoreApplication_Quit()
	})
	win.Show()
}

func (u *ui) refreshDevices() {
	prevSource := u.sourceCombo.CurrentText()
	prevDest := u.destCombo.CurrentText()

	go func() {
		cams, _ := enumerateCameras()
		lbs, _ := ListLoopbackDevices()
		mainthread.Start(func() {
			u.cameras = cams
			u.targets = lbs
			sourceLabels := cameraLabels(cams, "(no cameras)")
			destLabels := cameraLabels(lbs, "(no loopback devices found)")
			u.fillComboSelect(u.sourceCombo, sourceLabels, findLabelIndex(sourceLabels, prevSource))
			u.fillComboSelect(u.destCombo, destLabels, findLabelIndex(destLabels, prevDest))
			u.onSourceChanged(u.sourceCombo.CurrentIndex())
			u.tryAutoUpdatePipeline()
		})
	}()
}

func (u *ui) resetPipeline() {
	if u.loopback == nil {
		return
	}
	go func() {
		_ = u.loopback.Reset()
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
			})
		}()
		return
	}

	lb := u.loopback
	go func() {
		_ = lb.Reconfigure(cfg)
	}()
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

func cameraLabels(cams []Camera, empty string) []string {
	if len(cams) == 0 {
		return []string{empty}
	}
	labels := make([]string, len(cams))
	for i, c := range cams {
		labels[i] = c.Label()
	}
	return labels
}

func findLabelIndex(labels []string, prefer string) int {
	if prefer == "" || prefer == "(none)" {
		return 0
	}
	for i, label := range labels {
		if label == prefer {
			return i
		}
	}
	return 0
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

func themeIcon(names ...string) *qt6.QIcon {
	for _, name := range names {
		icon := qt6.QIcon_FromTheme(name)
		if !icon.IsNull() {
			return icon
		}
	}
	return qt6.NewQIcon()
}

func rowIconLabel(icon *qt6.QIcon) *qt6.QLabel {
	label := qt6.NewQLabel2()
	label.SetAlignment(qt6.AlignCenter)
	label.SetPixmap(icon.Pixmap2(24, 24))
	return label
}

func sourceRowIcon() *qt6.QIcon {
	return themeIcon("camera-video-symbolic", "camera-video")
}

func destRowIcon() *qt6.QIcon {
	icon := themeIcon("arrow-right-symbolic")
	if !icon.IsNull() {
		return icon
	}
	return qt6.QApplication_Style().StandardIcon(qt6.QStyle__SP_ArrowRight, nil, nil)
}

func refreshIcon() *qt6.QIcon {
	icon := themeIcon("view-refresh-symbolic", "view-refresh")
	if !icon.IsNull() {
		return icon
	}
	return qt6.QApplication_Style().StandardIcon(qt6.QStyle__SP_BrowserReload, nil, nil)
}

func configureRowIcon() *qt6.QIcon {
	return themeIcon("configure")
}

func cameraIcon() *qt6.QIcon {
	icon := themeIcon("camera-video", "camera-web", "camera-photo")
	if !icon.IsNull() {
		return icon
	}
	return qt6.NewQIcon()
}

func fpsLabel(fps float32) string {
	if fps == float32(int(fps)) {
		return strconv.Itoa(int(fps)) + " fps"
	}
	return strconv.FormatFloat(float64(fps), 'g', -1, 32) + " fps"
}
