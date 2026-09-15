package main

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/alexiusacademia/fynesimplechart"

	"github.com/Spritan/gitezz/internal/gitx"
)

var (
	detailHistoryGraph bool
	detailHistoryBody  *fyne.Container
)

func formatCommitTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	local := t.Local()
	ago := time.Since(local)
	switch {
	case ago < time.Minute:
		return "just now"
	case ago < time.Hour:
		return fmt.Sprintf("%dm ago", int(ago.Minutes()))
	case ago < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(ago.Hours()))
	case ago < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(ago.Hours()/24))
	case local.Year() == time.Now().Year():
		return local.Format("Jan 2, 3:04 PM")
	default:
		return local.Format("Jan 2, 2006")
	}
}

func historyLine(e gitx.LogEntry) string {
	subject := e.Subject
	if subject == "" {
		subject = "(no message)"
	}
	parts := []string{e.Hash, subject}
	if e.Author != "" {
		parts = append(parts, "·", e.Author)
	}
	if ts := formatCommitTime(e.When); ts != "" {
		parts = append(parts, "·", ts)
	}
	return strings.Join(parts, "  ")
}

func commitActivityChart(entries []gitx.LogEntry) fyne.CanvasObject {
	type dayPoint struct {
		key   string
		count int
	}
	order := make([]string, 0)
	counts := map[string]int{}
	// Log is newest-first; chart left→right should be oldest→newest.
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.When.IsZero() {
			continue
		}
		key := e.When.Local().Format("01/02")
		full := e.When.Local().Format("2006-01-02")
		if _, ok := counts[full]; !ok {
			order = append(order, full)
			_ = key
		}
		counts[full]++
	}
	if len(order) == 0 {
		return widget.NewLabel("No dated commits to chart")
	}

	nodes := make([]fynesimplechart.Node, 0, len(order))
	for i, full := range order {
		nodes = append(nodes, *fynesimplechart.NewNode(float32(i), float32(counts[full])))
	}

	plot := fynesimplechart.NewPlot(nodes, "Commits")
	plot.ShowBars = true
	plot.ShowPoints = false
	plot.ShowLine = true
	plot.LineWidth = 1.5
	plot.BarWidth = 0.65
	plot.PointSize = 3
	plot.PlotColor = color.NRGBA{R: 64, G: 158, B: 255, A: 255}
	plot.ShowDataLabels = len(order) <= 14
	plot.LabelFormat = "%.0f"

	chart := fynesimplechart.NewGraphWidget([]fynesimplechart.Plot{*plot})
	chart.SetChartTitle("Commit activity")
	chart.XAxisTitle = "Day"
	chart.YAxisTitle = "Commits"
	chart.ShowLegend = false
	chart.LegendPosition = fynesimplechart.LegendNone
	chart.ShowGrid = true
	chart.Resize(fyne.NewSize(420, 200))

	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(10, 200))
	return container.NewStack(spacer, chart)
}

func renderDetailHistory(entries []gitx.LogEntry) {
	if detailHistory == nil {
		return
	}
	detailHistory.Objects = nil
	if len(entries) == 0 {
		detailHistory.Add(widget.NewLabel("No commits"))
	} else {
		for _, e := range entries {
			lbl := widget.NewLabel(historyLine(e))
			lbl.TextStyle = fyne.TextStyle{Monospace: true}
			lbl.Truncation = fyne.TextTruncateEllipsis
			detailHistory.Add(lbl)
		}
	}
	detailHistory.Refresh()

	if detailHistoryBody == nil {
		return
	}
	list := container.NewVScroll(detailHistory)
	if detailHistoryGraph && len(entries) > 0 {
		detailHistoryBody.Objects = []fyne.CanvasObject{
			container.NewBorder(commitActivityChart(entries), nil, nil, nil, list),
		}
	} else {
		detailHistoryBody.Objects = []fyne.CanvasObject{list}
	}
	detailHistoryBody.Refresh()
}
