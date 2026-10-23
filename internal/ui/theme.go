package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Palette modelled on macOS dark appearance: near-black window, slightly
// raised cards, one blue accent.
var (
	colWindow     = color.NRGBA{0x17, 0x17, 0x19, 0xff}
	colCard       = color.NRGBA{0x22, 0x22, 0x25, 0xff}
	colCardBorder = color.NRGBA{0xff, 0xff, 0xff, 0x12}
	colRaised     = color.NRGBA{0x2c, 0x2c, 0x2f, 0xff}
	colText       = color.NRGBA{0xf2, 0xf2, 0xf7, 0xff}
	colSecondary  = color.NRGBA{0x98, 0x98, 0x9f, 0xff}
	colTertiary   = color.NRGBA{0x63, 0x63, 0x69, 0xff}
	colAccent     = color.NRGBA{0x0a, 0x84, 0xff, 0xff}
	colAccentSoft = color.NRGBA{0x0a, 0x84, 0xff, 0x38}
	colAccentWash = color.NRGBA{0x0a, 0x84, 0xff, 0x14}
	colSuccess    = color.NRGBA{0x30, 0xd1, 0x58, 0xff}
	colWarning    = color.NRGBA{0xff, 0xd6, 0x0a, 0xff}
	colDanger     = color.NRGBA{0xff, 0x45, 0x3a, 0xff}
	colTrack      = color.NRGBA{0xff, 0xff, 0xff, 0x14}
)

type nscpTheme struct{}

var _ fyne.Theme = nscpTheme{}

func (nscpTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return colWindow
	case theme.ColorNameForeground:
		return colText
	case theme.ColorNamePrimary, theme.ColorNameHyperlink:
		return colAccent
	case theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnError, theme.ColorNameForegroundOnSuccess:
		return color.White
	case theme.ColorNameForegroundOnWarning:
		return color.Black
	case theme.ColorNameButton:
		return colRaised
	case theme.ColorNameDisabledButton:
		return color.NRGBA{0x26, 0x26, 0x29, 0xff}
	case theme.ColorNameDisabled:
		return colTertiary
	case theme.ColorNamePlaceHolder:
		return colSecondary
	case theme.ColorNameHover:
		return color.NRGBA{0xff, 0xff, 0xff, 0x0f}
	case theme.ColorNamePressed:
		return color.NRGBA{0xff, 0xff, 0xff, 0x1a}
	case theme.ColorNameFocus:
		return color.NRGBA{0x0a, 0x84, 0xff, 0x80}
	case theme.ColorNameSelection:
		return colAccentSoft
	case theme.ColorNameInputBackground:
		return color.NRGBA{0x1b, 0x1b, 0x1e, 0xff}
	case theme.ColorNameInputBorder:
		return color.NRGBA{0xff, 0xff, 0xff, 0x1a}
	case theme.ColorNameSeparator:
		return color.NRGBA{0xff, 0xff, 0xff, 0x10}
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return color.NRGBA{0x26, 0x26, 0x29, 0xff}
	case theme.ColorNameHeaderBackground:
		return colCard
	case theme.ColorNameShadow:
		return color.NRGBA{0x00, 0x00, 0x00, 0x66}
	case theme.ColorNameScrollBar:
		return color.NRGBA{0xff, 0xff, 0xff, 0x40}
	case theme.ColorNameScrollBarBackground:
		return color.Transparent
	case theme.ColorNameSuccess:
		return colSuccess
	case theme.ColorNameWarning:
		return colWarning
	case theme.ColorNameError:
		return colDanger
	}
	return theme.DefaultTheme().Color(n, theme.VariantDark)
}

func (nscpTheme) Font(s fyne.TextStyle) fyne.Resource { return theme.DefaultTheme().Font(s) }

func (nscpTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	switch n {
	case theme.IconNameNavigateNext:
		return theme.NewThemedResource(chevronRight)
	case theme.IconNameMoveDown:
		return theme.NewThemedResource(chevronDown)
	}
	return theme.DefaultTheme().Icon(n)
}

func (nscpTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 13
	case theme.SizeNameCaptionText:
		return 11
	case theme.SizeNameSubHeadingText:
		return 15
	case theme.SizeNameHeadingText:
		return 20
	case theme.SizeNamePadding:
		return 4
	case theme.SizeNameInnerPadding:
		return 7
	case theme.SizeNameLineSpacing:
		return 4
	case theme.SizeNameInlineIcon:
		return 16
	case theme.SizeNameInputRadius:
		return 7
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameScrollBar:
		return 6
	case theme.SizeNameScrollBarSmall:
		return 3
	case theme.SizeNameScrollBarRadius:
		return 3
	case theme.SizeNameSeparatorThickness:
		return 1
	case theme.SizeNameInputBorder:
		return 1
	}
	return theme.DefaultTheme().Size(n)
}

var chevronRight = fyne.NewStaticResource("chevron-right.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#000000" d="M9.3 5.3a1 1 0 0 1 1.4 0l6 6a1 1 0 0 1 0 1.4l-6 6a1 1 0 1 1-1.4-1.4l5.29-5.3-5.3-5.3a1 1 0 0 1 0-1.4z"/></svg>`))

var chevronDown = fyne.NewStaticResource("chevron-down.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#000000" d="M5.3 9.3a1 1 0 0 1 1.4 0l5.3 5.29 5.3-5.3a1 1 0 1 1 1.4 1.42l-6 6a1 1 0 0 1-1.4 0l-6-6a1 1 0 0 1 0-1.42z"/></svg>`))
