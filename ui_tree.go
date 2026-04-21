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
	drawString(screen, 0, row, rootLabel, tcell.StyleDefault.Bold(true))
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

	var label string
	if node.IsFile {
		label = fmt.Sprintf("%s%s%s  %s", prefix, branch, node.Name, formatSize(node.Size))
	} else {
		label = fmt.Sprintf("%s%s%s/", prefix, branch, node.Name)
	}

	drawString(screen, 0, *row, label, tcell.StyleDefault)
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
