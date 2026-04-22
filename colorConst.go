package main

import (
	"image/color"
	"time"
)

const (
	// ChangingColorTime — длительность плавного перехода цвета к цвету по умолчанию.
	ChangingColorTime = 2 * time.Second
	// ChangingColorTimeDiff — то же в режиме отображения Diff (дерево только с IsUpdating).
	ChangingColorTimeDiff = 3 * time.Second
	// UpdateTreePeriod — период обхода дерева и пересчёта цветов.
	UpdateTreePeriod = 100 * time.Millisecond
)

// Все цвета держим вместе (hex sRGB, 0xRRGGBB).
const (
	defaultFileTextColorHex = 0xCCCCCC
	defaultDirTextColorHex  = 0x6FAFBD
	changedFileColorHex     = 0xFCFA31
	removedColorHex         = 0xD63638
	// Цвет, к которому плавно переходит узел при удалении (перед исчезновением из дерева).
	removedFadeTargetColorHex = 0x393939
	pseudoGraphicsColorHex    = 0x343635
	fileSizeTextColorHex      = 0x797D84
)

func colorFromHexRGB(hex uint32) color.NRGBA {
	return color.NRGBA{
		R: uint8((hex >> 16) & 0xFF),
		G: uint8((hex >> 8) & 0xFF),
		B: uint8(hex & 0xFF),
		A: 0xFF,
	}
}

var (
	defaultFileTextColor   = colorFromHexRGB(defaultFileTextColorHex)
	defaultDirTextColor    = colorFromHexRGB(defaultDirTextColorHex)
	changedFileColor       = colorFromHexRGB(changedFileColorHex)
	removedColor           = colorFromHexRGB(removedColorHex)
	removedFadeTargetColor = colorFromHexRGB(removedFadeTargetColorHex)
	pseudoGraphicsColor    = colorFromHexRGB(pseudoGraphicsColorHex)
	fileSizeTextColor      = colorFromHexRGB(fileSizeTextColorHex)
)
