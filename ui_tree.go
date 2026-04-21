package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/gdamore/tcell/v2"
)

// RunTreeUI сканирует rootFolder, отрисовывает дерево и обрабатывает события.
// Применяет входящие FileChange к дереву и перерисовывает экран.
// Завершается при нажатии q / Escape / Ctrl+C или при отмене ctx.
func RunTreeUI(
	ctx context.Context,
	cancel context.CancelFunc,
	rootFolder string,
	updates <-chan FileChange,
) {
	tree, err := ScanDir(rootFolder)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка сканирования каталога: %v\n", err)
		os.Exit(1)
	}

	screen, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка инициализации экрана: %v\n", err)
		os.Exit(1)
	}
	if err := screen.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка инициализации экрана: %v\n", err)
		os.Exit(1)
	}
	defer screen.Fini()

	var mu sync.RWMutex
	repaintCh := make(chan struct{}, 1)
	anim := newTreeColorAnimator(tree, &mu, ctx, repaintCh)

	screen.Clear()
	renderTree(screen, tree, &mu)
	screen.Show()

	events := make(chan tcell.Event, 16)
	go func() {
		for {
			ev := screen.PollEvent()
			if ev == nil {
				return
			}
			select {
			case events <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case ch := <-updates:
			mu.Lock()
			ApplyChange(tree, ch)
			mu.Unlock()
			anim.Notify()

			screen.Clear()
			renderTree(screen, tree, &mu)
			screen.Show()

		case <-repaintCh:
			screen.Clear()
			renderTree(screen, tree, &mu)
			screen.Show()

		case ev := <-events:
			switch e := ev.(type) {
			case *tcell.EventResize:
				screen.Sync()
				screen.Clear()
				renderTree(screen, tree, &mu)
				screen.Show()

			case *tcell.EventKey:
				if isQuitKey(e) {
					cancel()
					return
				}
			}
		}
	}
}

// isQuitKey возвращает true для q, Escape и Ctrl+C.
func isQuitKey(e *tcell.EventKey) bool {
	return e.Key() == tcell.KeyEscape ||
		e.Key() == tcell.KeyCtrlC ||
		(e.Key() == tcell.KeyRune && e.Rune() == 'q')
}

// renderTree отрисовывает дерево DiskItemInfo на экране построчно.
func renderTree(screen tcell.Screen, root *DiskItemInfo, mu *sync.RWMutex) {
	mu.RLock()
	defer mu.RUnlock()

	row := 0
	rootLabel := root.Name + "/"
	drawString(screen, 0, row, rootLabel, tcell.StyleDefault.Bold(true).Foreground(
		tcell.NewRGBColor(int32(defaultDirTextColor.R), int32(defaultDirTextColor.G), int32(defaultDirTextColor.B)),
	))
	row++

	// Для потомков корня начинаем с пустого префикса и рассчитываем,
	// является ли элемент последним, чтобы выбрать ├─ или └─.
	children := sortedItems(root.Items)
	for i, child := range children {
		drawItem(screen, child, "", i == len(children)-1, &row)
	}
}

// drawItem рекурсивно рисует узел с псевдографикой.
func drawItem(screen tcell.Screen, node *DiskItemInfo, prefix string, isLast bool, row *int) {
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
	for i, child := range children {
		drawItem(screen, child, nextPrefix, i == len(children)-1, row)
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
