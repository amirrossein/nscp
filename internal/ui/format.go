package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
)

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// runOnMain marshals UI updates from background goroutines onto the Fyne
// main goroutine; tests replace it to run callbacks deterministically.
var runOnMain = fyne.Do
