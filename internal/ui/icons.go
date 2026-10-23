package ui

import (
	"path"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

func muted(r fyne.Resource) fyne.Resource {
	return theme.NewColoredResource(r, theme.ColorNamePlaceHolder)
}

func iconFor(name string, isDir, isLink bool) fyne.Resource {
	if isDir {
		if isLink {
			return theme.NewPrimaryThemedResource(theme.FolderOpenIcon())
		}
		return theme.NewPrimaryThemedResource(theme.FolderIcon())
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".heic", ".bmp", ".tiff":
		return muted(theme.FileImageIcon())
	case ".mp4", ".mov", ".mkv", ".avi", ".webm":
		return muted(theme.FileVideoIcon())
	case ".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a":
		return muted(theme.FileAudioIcon())
	case ".txt", ".md", ".log", ".csv", ".json", ".yaml", ".yml", ".toml", ".xml", ".conf", ".ini",
		".go", ".py", ".js", ".ts", ".sh", ".rs", ".c", ".h", ".java", ".rb", ".php", ".html", ".css", ".sql":
		return muted(theme.FileTextIcon())
	case ".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".7z", ".dmg", ".pkg", ".deb", ".rpm", ".exe", ".bin", ".pdf":
		return muted(theme.FileApplicationIcon())
	}
	return muted(theme.FileIcon())
}

func shortTime(t time.Time) string {
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "Today " + t.Format("15:04")
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday " + t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("Jan 2, 15:04")
	}
	return t.Format("Jan 2, 2006")
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
