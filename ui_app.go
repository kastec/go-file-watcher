package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/gdamore/tcell/v2"
)

// appViewMode — текущий режим отображения приложения.
type appViewMode int

const (
	appViewList appViewMode = iota // журнал изменений (как ui_list)
	appViewTree                    // полное дерево
	appViewTreeDiff                // дерево: только ветки с IsUpdating
)

// RunMainUI — основной цикл: дерево по умолчанию, переключение 1/2/3, выход q/Esc/Ctrl+C.
func RunMainUI(
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
	var changeLog []string
	const changeLogMax = 2000

	repaintCh := make(chan struct{}, 1)
	anim := newTreeColorAnimator(tree, &mu, ctx, repaintCh)

	mode := appViewTree

	redraw := func() {
		if mode == appViewTreeDiff {
			anim.SetBlendDuration(ChangingColorTimeDiff)
		} else {
			anim.SetBlendDuration(ChangingColorTime)
		}
		screen.Clear()
		switch mode {
		case appViewList:
			renderChangeLog(screen, changeLog)
		case appViewTree:
			renderTree(screen, tree, &mu, false)
		case appViewTreeDiff:
			renderTree(screen, tree, &mu, true)
		}
		drawStatusLine(screen, mode)
		screen.Show()
	}

	redraw()

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
			line := formatChangeLine(ch)
			mu.Lock()
			ApplyChange(tree, ch)
			changeLog = append(changeLog, line)
			if len(changeLog) > changeLogMax {
				changeLog = changeLog[len(changeLog)-changeLogMax:]
			}
			mu.Unlock()
			anim.Notify()

			redraw()

		case <-repaintCh:
			redraw()

		case ev := <-events:
			switch e := ev.(type) {
			case *tcell.EventResize:
				screen.Sync()
				redraw()

			case *tcell.EventKey:
				if isQuitKey(e) {
					cancel()
					return
				}
				if e.Key() == tcell.KeyRune {
					switch e.Rune() {
					case '1':
						mode = appViewList
						redraw()
					case '2':
						mode = appViewTree
						redraw()
					case '3':
						mode = appViewTreeDiff
						redraw()
					}
				}
			}
		}
	}
}

func drawStatusLine(screen tcell.Screen, mode appViewMode) {
	_, h := screen.Size()
	if h <= 0 {
		return
	}
	y := h - 1
	label := "1=list  2=tree  3=diff  q/Esc/Ctrl+C=exit"
	switch mode {
	case appViewList:
		label = "[список] " + label
	case appViewTree:
		label = "[дерево] " + label
	case appViewTreeDiff:
		label = "[дерево diff] " + label
	}
	w, _ := screen.Size()
	style := tcell.StyleDefault.Reverse(true)
	for x := 0; x < w; x++ {
		screen.SetContent(x, y, ' ', nil, style)
	}
	drawString(screen, 0, y, label, style)
}

// renderChangeLog рисует журнал событий; последняя строка у нижней границы над статусом.
func renderChangeLog(screen tcell.Screen, lines []string) {
	w, h := screen.Size()
	if w <= 0 || h <= 1 {
		return
	}
	contentRows := h - 1
	if contentRows <= 0 {
		return
	}
	if len(lines) == 0 {
		drawString(screen, 0, 0, "Пока нет событий изменений.", tcell.StyleDefault)
		return
	}
	start := 0
	if len(lines) > contentRows {
		start = len(lines) - contentRows
	}
	for i, line := range lines[start:] {
		if i >= contentRows {
			break
		}
		style := tcell.StyleDefault
		drawStringClipped(screen, 0, i, line, w, style)
	}
}

// drawStringClipped обрезает строку по ширине экрана (по рунам).
func drawStringClipped(screen tcell.Screen, x, y int, s string, maxWidth int, style tcell.Style) {
	col := x
	for _, r := range s {
		if col >= maxWidth {
			break
		}
		screen.SetContent(col, y, r, nil, style)
		col++
	}
}
