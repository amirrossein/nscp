package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type conflictChoice int

const (
	choiceOverwrite conflictChoice = iota
	choiceSkip
	choiceOverwriteAll
	choiceCancel
)

// resolveConflicts asks about each item whose destination exists, one dialog
// at a time, then calls done with the items to transfer.
func resolveConflicts(win fyne.Window, items []pendingItem, done func([]pendingItem)) {
	var chosen []pendingItem
	overwriteAll := false
	var step func(i int)
	step = func(i int) {
		for ; i < len(items); i++ {
			it := items[i]
			if !it.exists {
				chosen = append(chosen, it)
				continue
			}
			if overwriteAll {
				it.overwrite = true
				chosen = append(chosen, it)
				continue
			}
			askConflict(win, it, len(items)-i > 1, func(c conflictChoice) {
				switch c {
				case choiceCancel:
					done(nil)
					return
				case choiceOverwriteAll:
					overwriteAll = true
					fallthrough
				case choiceOverwrite:
					it.overwrite = true
					chosen = append(chosen, it)
				}
				step(i + 1)
			})
			return
		}
		done(chosen)
	}
	step(0)
}

func askConflict(win fyne.Window, it pendingItem, more bool, answer func(conflictChoice)) {
	kind := "file"
	if it.existing.IsDir {
		kind = "folder"
	}
	msg := fmt.Sprintf("A %s named %q already exists in the destination.", kind, it.entry.Name)
	switch {
	case it.existing.IsDir && it.entry.IsDir:
		msg += "\n\nOverwrite merges the folders, replacing files with the same name."
	case it.existing.IsDir != it.entry.IsDir:
		msg += "\n\nOverwrite deletes the existing " + kind + " first."
	}
	var d *dialog.CustomDialog
	btn := func(label string, c conflictChoice, imp widget.Importance) fyne.CanvasObject {
		b := widget.NewButton(label, func() {
			d.Hide()
			answer(c)
		})
		b.Importance = imp
		return b
	}
	buttons := []fyne.CanvasObject{
		btn("Cancel", choiceCancel, widget.MediumImportance),
		btn("Skip", choiceSkip, widget.MediumImportance),
	}
	if more {
		buttons = append(buttons, btn("Overwrite all", choiceOverwriteAll, widget.DangerImportance))
	}
	buttons = append(buttons, btn("Overwrite", choiceOverwrite, widget.DangerImportance))
	label := widget.NewLabel(msg)
	label.Wrapping = fyne.TextWrapWord
	d = dialog.NewCustomWithoutButtons("Item already exists", label, win)
	d.SetButtons(buttons)
	d.Resize(fyne.NewSize(480, d.MinSize().Height+40))
	d.Show()
}
