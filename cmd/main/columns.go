package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Preferred widths for: Repository, Branch, Origin, Actions, Status.
var colPrefs = [5]float32{220, 170, 110, 250, 200}

var colMins = [5]float32{90, 90, 70, 170, 110}

var headerRow fyne.CanvasObject

const (
	colCheckW float32 = 28
	colGap    float32 = 8
)

func contentColWidths(total float32) [5]float32 {
	avail := total - colCheckW - colGap*5
	if avail < 160 {
		avail = 160
	}

	prefs := colPrefs
	sum := float32(0)
	for _, w := range prefs {
		sum += w
	}
	if sum <= 0 {
		sum = 1
	}

	var out [5]float32
	if sum < avail {
		out = prefs
		out[0] += avail - sum
		return out
	}

	scale := avail / sum
	used := float32(0)
	for i := range prefs {
		out[i] = prefs[i] * scale
		if out[i] < colMins[i]*0.85 {
			out[i] = colMins[i] * 0.85
		}
		used += out[i]
	}
	if used > avail {
		// Scale again to fit after min clamps.
		scale = avail / used
		for i := range out {
			out[i] *= scale
		}
	} else if used < avail {
		out[0] += avail - used
	}
	return out
}

func refreshColumnLayout() {
	if headerRow != nil {
		headerRow.Refresh()
	}
	if repoContainer != nil {
		repoContainer.Refresh()
	}
}

func headerCell(text string, resizeAfter int) fyne.CanvasObject {
	label := widget.NewLabel(text)
	label.TextStyle = fyne.TextStyle{Bold: true}
	label.Importance = widget.MediumImportance
	if resizeAfter < 0 || resizeAfter >= len(colPrefs)-1 {
		return label
	}
	handle := newColDragHandle(resizeAfter)
	return container.NewBorder(nil, nil, nil, handle, label)
}

type colDragHandle struct {
	widget.BaseWidget
	index  int
	bar    *canvas.Rectangle
	hovered bool
}

func newColDragHandle(index int) *colDragHandle {
	h := &colDragHandle{index: index}
	h.ExtendBaseWidget(h)
	return h
}

func (h *colDragHandle) CreateRenderer() fyne.WidgetRenderer {
	h.bar = canvas.NewRectangle(color.NRGBA{R: 70, G: 72, B: 82, A: 160})
	h.bar.CornerRadius = 1
	return widget.NewSimpleRenderer(h.bar)
}

func (h *colDragHandle) MinSize() fyne.Size {
	return fyne.NewSize(6, 18)
}

func (h *colDragHandle) Cursor() desktop.Cursor {
	return desktop.HResizeCursor
}

func (h *colDragHandle) MouseIn(*desktop.MouseEvent) {
	h.hovered = true
	if h.bar != nil {
		h.bar.FillColor = theme.Color(theme.ColorNamePrimary)
		h.bar.Refresh()
	}
}

func (h *colDragHandle) MouseOut() {
	h.hovered = false
	if h.bar != nil {
		h.bar.FillColor = color.NRGBA{R: 70, G: 72, B: 82, A: 160}
		h.bar.Refresh()
	}
}

func (h *colDragHandle) MouseMoved(*desktop.MouseEvent) {}

func (h *colDragHandle) Dragged(e *fyne.DragEvent) {
	dx := e.Dragged.DX
	if dx == 0 {
		return
	}
	left := h.index
	right := h.index + 1
	newL := colPrefs[left] + dx
	newR := colPrefs[right] - dx
	if newL < colMins[left] || newR < colMins[right] {
		return
	}
	colPrefs[left] = newL
	colPrefs[right] = newR
	refreshColumnLayout()
}

func (h *colDragHandle) DragEnd() {}
