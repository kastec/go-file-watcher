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
	var changeLog []FileChange
	const changeLogMax = 2000

	repaintCh := make(chan struct{}, 1)
	anim := newTreeColorAnimator(tree, &mu, ctx, repaintCh)

	mode := appViewTree
	prevViewMode := mode
	var verticalOffset int
	var lastRenderedTreeLines int
	listView := newListJournalView()

	redraw := func() {
		if mode == appViewList && prevViewMode != appViewList {
			listView.resetScroll()
		}
		if (mode == appViewTree || mode == appViewTreeDiff) && prevViewMode != mode {
			verticalOffset = 0
			lastRenderedTreeLines = 0
		}
		if mode == appViewTreeDiff {
			anim.SetBlendDuration(ChangingColorTimeDiff)
		} else {
			anim.SetBlendDuration(ChangingColorTime)
		}
		screen.Clear()
		switch mode {
		case appViewList:
			renderChangeLog(screen, changeLog, listView)
		case appViewTree:
			renderTree(screen, tree, &mu, false, &verticalOffset, &lastRenderedTreeLines)
		case appViewTreeDiff:
			renderTree(screen, tree, &mu, true, &verticalOffset, &lastRenderedTreeLines)
		}
		drawStatusLine(screen, mode)
		screen.Show()
		prevViewMode = mode
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
			mu.Lock()
			// Removed: в watcher IsFile не заполняется (файла уже нет на диске).
			// Берём признак из дерева, иначе каталог ошибочно рисуется в квадратных скобках как путь.
			if ch.ChangeType == Removed {
				if n := FindNode(tree, ch.FullPath); n != nil {
					ch.IsFile = n.IsFile
				}
			}
			ApplyChange(tree, ch)
			changeLog = append(changeLog, ch)
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
				switch e.Key() {
				case tcell.KeyUp:
					if mode == appViewList {
						mu.RLock()
						n := len(changeLog)
						mu.RUnlock()
						_, kh := screen.Size()
						if listView.handleScrollKeyUp(n, kh-1) {
							redraw()
						}
					} else if mode == appViewTree || mode == appViewTreeDiff {
						if verticalOffset > 0 {
							verticalOffset--
							redraw()
						}
					}
				case tcell.KeyDown:
					if mode == appViewList {
						mu.RLock()
						n := len(changeLog)
						mu.RUnlock()
						_, kh := screen.Size()
						if listView.handleScrollKeyDown(n, kh-1) {
							redraw()
						}
					} else if mode == appViewTree || mode == appViewTreeDiff {
						verticalOffset++
						redraw()
					}
				case tcell.KeyPgUp:
					if mode == appViewList {
						mu.RLock()
						n := len(changeLog)
						mu.RUnlock()
						_, kh := screen.Size()
						if listView.handleScrollPageUp(n, kh-1) {
							redraw()
						}
					} else if mode == appViewTree || mode == appViewTreeDiff {
						_, kh := screen.Size()
						page := kh - 1
						if page > 0 && verticalOffset > 0 {
							verticalOffset -= page
							if verticalOffset < 0 {
								verticalOffset = 0
							}
							redraw()
						}
					}
				case tcell.KeyPgDn:
					if mode == appViewList {
						mu.RLock()
						n := len(changeLog)
						mu.RUnlock()
						_, kh := screen.Size()
						if listView.handleScrollPageDown(n, kh-1) {
							redraw()
						}
					} else if mode == appViewTree || mode == appViewTreeDiff {
						_, kh := screen.Size()
						page := kh - 1
						if page > 0 {
							verticalOffset += page
							redraw()
						}
					}
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
