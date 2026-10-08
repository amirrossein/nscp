package ui

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/amirrossein/nscp/internal/transfer"
)

type transfersPanel struct {
	m       *MainWindow
	jobs    []*transfer.Job
	list    *widget.List
	summary *widget.Label
	content fyne.CanvasObject
	done    chan struct{}
}

func newTransfersPanel(m *MainWindow) *transfersPanel {
	t := &transfersPanel{m: m, done: make(chan struct{})}
	t.list = widget.NewList(
		func() int { return len(t.jobs) },
		t.createRow,
		t.updateRow,
	)
	t.list.OnSelected = func(id widget.ListItemID) {
		t.list.UnselectAll()
		if id < len(t.jobs) {
			t.showDetails(t.jobs[id])
		}
	}
	t.summary = widget.NewLabel("No transfers")
	clear := widget.NewButtonWithIcon("Clear finished", theme.DeleteIcon(), func() {
		m.queue.ClearFinished()
		t.refresh()
	})
	header := container.NewBorder(nil, nil, widget.NewLabelWithStyle("Transfers", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), clear, t.summary)
	t.content = container.NewBorder(header, nil, nil, nil, t.list)
	go t.tick()
	return t
}

func (t *transfersPanel) stop() { close(t.done) }

func (t *transfersPanel) tick() {
	tk := time.NewTicker(200 * time.Millisecond)
	defer tk.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-tk.C:
			runOnMain(func() {
				for _, j := range t.jobs {
					if !j.Status().Terminal() {
						t.refresh()
						return
					}
				}
			})
		}
	}
}

func (t *transfersPanel) refresh() {
	t.jobs = t.m.queue.Jobs()
	active := 0
	for _, j := range t.jobs {
		if !j.Status().Terminal() {
			active++
		}
	}
	switch {
	case len(t.jobs) == 0:
		t.summary.SetText("No transfers")
	case active > 0:
		t.summary.SetText(fmt.Sprintf("%d active", active))
	default:
		t.summary.SetText("All done")
	}
	t.list.Refresh()
}

type jobRow struct {
	icon   *widget.Icon
	title  *widget.Label
	detail *widget.Label
	bar    *widget.ProgressBar
	cancel *widget.Button
}

func (t *transfersPanel) createRow() fyne.CanvasObject {
	r := &jobRow{
		icon:   widget.NewIcon(theme.UploadIcon()),
		title:  widget.NewLabel(""),
		detail: widget.NewLabel(""),
		bar:    widget.NewProgressBar(),
		cancel: widget.NewButtonWithIcon("", theme.CancelIcon(), nil),
	}
	r.title.Truncation = fyne.TextTruncateEllipsis
	r.detail.Truncation = fyne.TextTruncateEllipsis
	r.detail.Importance = widget.LowImportance
	barBox := container.NewGridWrap(fyne.NewSize(180, r.bar.MinSize().Height), r.bar)
	right := container.NewHBox(container.NewCenter(barBox), r.cancel)
	body := container.NewGridWithColumns(2, r.title, r.detail)
	c := container.NewBorder(nil, nil, r.icon, right, body)
	return &rowHolder{Container: c, row: r}
}

type rowHolder struct {
	*fyne.Container
	row *jobRow
}

func (t *transfersPanel) updateRow(id widget.ListItemID, o fyne.CanvasObject) {
	if id >= len(t.jobs) {
		return
	}
	j := t.jobs[id]
	r := o.(*rowHolder).row
	s := j.Snapshot()

	r.title.SetText(fmt.Sprintf("%s  →  %s:%s", j.Source.Name, j.Dst.Label(), j.DstDir))
	if s.BytesTotal > 0 {
		r.bar.SetValue(float64(s.BytesDone) / float64(s.BytesTotal))
	} else if s.Status == transfer.Done {
		r.bar.SetValue(1)
	} else {
		r.bar.SetValue(0)
	}
	r.detail.Importance = widget.LowImportance
	switch s.Status {
	case transfer.Queued:
		r.icon.SetResource(theme.HistoryIcon())
		r.detail.SetText("Queued")
	case transfer.Running:
		r.icon.SetResource(theme.MediaPlayIcon())
		text := fmt.Sprintf("%s / %s", humanBytes(s.BytesDone), humanBytes(s.BytesTotal))
		if s.FilesTotal > 1 {
			text = fmt.Sprintf("%d/%d files · %s · %s", s.FilesDone, s.FilesTotal, text, s.Current)
		}
		r.detail.SetText(text)
	case transfer.Done:
		r.icon.SetResource(theme.ConfirmIcon())
		text := "Done · " + humanBytes(s.BytesTotal)
		if n := len(s.Warnings); n > 0 {
			text += fmt.Sprintf(" · %d warning(s)", n)
			r.detail.Importance = widget.WarningImportance
		}
		r.detail.SetText(text)
	case transfer.Failed:
		r.icon.SetResource(theme.ErrorIcon())
		r.detail.Importance = widget.DangerImportance
		r.detail.SetText(firstLine(s.Err.Error()))
	case transfer.Cancelled:
		r.icon.SetResource(theme.CancelIcon())
		r.detail.SetText("Cancelled")
	}
	r.detail.Refresh()
	if s.Status.Terminal() {
		r.cancel.Hide()
	} else {
		r.cancel.Show()
		r.cancel.OnTapped = j.Cancel
	}
}

func (t *transfersPanel) showDetails(j *transfer.Job) {
	s := j.Snapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s:%s\nTo: %s:%s\nStatus: %s\n", j.Src.Label(), j.Source.Path, j.Dst.Label(), j.DstPath(), s.Status)
	fmt.Fprintf(&b, "Files: %d/%d\nBytes: %s / %s\n", s.FilesDone, s.FilesTotal, humanBytes(s.BytesDone), humanBytes(s.BytesTotal))
	if s.Err != nil {
		fmt.Fprintf(&b, "\nError: %v\n", s.Err)
	}
	if len(s.Warnings) > 0 {
		b.WriteString("\nWarnings:\n")
		for _, w := range s.Warnings {
			b.WriteString("• " + w + "\n")
		}
	}
	text := widget.NewLabel(b.String())
	text.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(text)
	scroll.SetMinSize(fyne.NewSize(520, 220))
	dialog.ShowCustom("Transfer details", "Close", scroll, t.m.win)
}
