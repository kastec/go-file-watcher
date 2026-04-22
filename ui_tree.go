package main

import (
	"fmt"
	"sort"
	"sync"

	"github.com/gdamore/tcell/v2"
)

// isQuitKey возвращает true для q, Escape и Ctrl+C.
func isQuitKey(e *tcell.EventKey) bool {
	return e.Key() == tcell.KeyEscape ||
		e.Key() == tcell.KeyCtrlC ||
		(e.Key() == tcell.KeyRune && e.Rune() == 'q')
}

// visibleInDiffTree — узел показывается в режиме diff, если сам IsUpdating
// или среди потомков есть узел с IsUpdating (чтобы сохранить путь в дереве).
func visibleInDiffTree(n *DiskItemInfo) bool {
	if n.IsUpdating {
		return true
	}
	for _, c := range n.Items {
		if visibleInDiffTree(c) {
			return true
		}
	}
	return false
}

func filterItemsForDiff(items []*DiskItemInfo) []*DiskItemInfo {
	var out []*DiskItemInfo
	for _, it := range sortedItems(items) {
		if visibleInDiffTree(it) {
			out = append(out, it)
		}
	}
	return out
}

// renderTree отрисовывает дерево DiskItemInfo на экране построчно.
// Если diffOnly, выводятся только ветки, где у узла или потомка IsUpdating.
func renderTree(screen tcell.Screen, root *DiskItemInfo, mu *sync.RWMutex, diffOnly bool) {
	mu.RLock()
	defer mu.RUnlock()

	row := 0
	rootLabel := root.Name + "/"
	drawString(screen, 0, row, rootLabel, tcell.StyleDefault.Bold(true).Foreground(
		tcell.NewRGBColor(int32(defaultDirTextColor.R), int32(defaultDirTextColor.G), int32(defaultDirTextColor.B)),
	))
	row++

	children := sortedItems(root.Items)
	if diffOnly {
		children = filterItemsForDiff(children)
	}
	if diffOnly && len(children) == 0 {
		drawString(screen, 0, row, "(нет элементов с IsUpdating)", tcell.StyleDefault.Dim(true))
		return
	}

	for i, child := range children {
		drawItem(screen, child, "", i == len(children)-1, &row, diffOnly)
	}
}

// drawItem рекурсивно рисует узел с псевдографикой.
func drawItem(screen tcell.Screen, node *DiskItemInfo, prefix string, isLast bool, row *int, diffOnly bool) {
	branch := "├─"
	nextPrefix := prefix + "│ "
	if isLast {
		branch = "└─"
		nextPrefix = prefix + "  "
	}

	prefixPart := prefix + branch
	x := runeLen(prefixPart)
	drawString(screen, 0, *row, prefixPart, pseudoStyle())
	if node.IsFile {
		namePart := node.Name
		drawString(screen, x, *row, namePart, itemStyle(node))
		x += runeLen(namePart)
		sep := "  "
		drawString(screen, x, *row, sep, itemStyle(node))
		x += runeLen(sep)
		drawString(screen, x, *row, formatSize(node.Size), fileSizeStyle())
	} else {
		drawString(screen, x, *row, fmt.Sprintf("%s/", node.Name), itemStyle(node))
	}
	*row++

	children := sortedItems(node.Items)
	if diffOnly {
		children = filterItemsForDiff(children)
	}
	for i, child := range children {
		drawItem(screen, child, nextPrefix, i == len(children)-1, row, diffOnly)
	}
}

// splitItems разделяет Items на каталоги и файлы, каждую группу сортирует по имени.
func splitItems(items []*DiskItemInfo) (dirs, files []*DiskItemInfo) {
	for _, item := range items {
		if item.IsFile {
			files = append(files, item)
		} else {
			dirs = append(dirs, item)
		}
	}

	sortItems(dirs)
	sortItems(files)
	return
}

func sortedItems(items []*DiskItemInfo) []*DiskItemInfo {
	dirs, files := splitItems(items)
	return append(dirs, files...)
}

func sortItems(items []*DiskItemInfo) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
}

func itemStyle(node *DiskItemInfo) tcell.Style {
	c := node.Color
	if c == nil {
		if node.IsFile {
			c = defaultFileTextColor
		} else {
			c = defaultDirTextColor
		}
	}
	r16, g16, b16, _ := c.RGBA()
	return tcell.StyleDefault.Foreground(
		tcell.NewRGBColor(int32(r16>>8), int32(g16>>8), int32(b16>>8)),
	)
}

func pseudoStyle() tcell.Style {
	r16, g16, b16, _ := pseudoGraphicsColor.RGBA()
	return tcell.StyleDefault.Foreground(
		tcell.NewRGBColor(int32(r16>>8), int32(g16>>8), int32(b16>>8)),
	)
}

func fileSizeStyle() tcell.Style {
	r16, g16, b16, _ := fileSizeTextColor.RGBA()
	return tcell.StyleDefault.Foreground(
		tcell.NewRGBColor(int32(r16>>8), int32(g16>>8), int32(b16>>8)),
	)
}

func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// drawString выводит строку s на экран начиная с позиции (x, y).
func drawString(screen tcell.Screen, x, y int, s string, style tcell.Style) {
	col := x
	for _, r := range s {
		screen.SetContent(col, y, r, nil, style)
		col++
	}
}

// formatSize возвращает человекочитаемый размер файла.
func formatSize(size int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case size >= gb:
		return fmt.Sprintf("%.1f Gb", float64(size)/gb)
	case size >= mb:
		return fmt.Sprintf("%.1f Mb", float64(size)/mb)
	case size >= kb:
		return fmt.Sprintf("%.1f Kb", float64(size)/kb)
	default:
		return fmt.Sprintf("%d b", size)
	}
}
