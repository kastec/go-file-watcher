package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gdamore/tcell/v2"
)

// appViewMode — текущий режим отображения приложения.
type appViewMode int

const (
	appViewList     appViewMode = iota // журнал изменений (как ui_list)
	appViewTree                        // полное дерево
	appViewTreeDiff                    // дерево: только ветки с IsUpdating
)

// Состояние основного UI (один экземпляр на процесс; заполняется в RunMainUI).
var (
	tScreen               tcell.Screen
	uiMutex               sync.RWMutex
	changeLog             []FileChange
	viewMode              appViewMode
	prevViewMode          appViewMode
	mainUIListView        *listJournalView
	verticalOffset        int
	mainUITree            *DiskItemInfo
	colAnimator           *treeColorAnimator
	lastRenderedTreeLines int
)

func mainRedraw() {
	if viewMode == appViewList && prevViewMode != appViewList {
		mainUIListView.resetScroll()
	}
	if (viewMode == appViewTree || viewMode == appViewTreeDiff) && prevViewMode != viewMode {
		verticalOffset = 0
		lastRenderedTreeLines = 0
	}
	if viewMode == appViewTreeDiff {
		colAnimator.SetBlendDuration(ChangingColorTimeDiff)
	} else {
		colAnimator.SetBlendDuration(ChangingColorTime)
	}
	tScreen.Clear()
	switch viewMode {
	case appViewList:
		renderChangeLog(tScreen, changeLog, mainUIListView)
	case appViewTree:
		renderTree(tScreen, mainUITree, &uiMutex, false, &verticalOffset, &lastRenderedTreeLines)
	case appViewTreeDiff:
		renderTree(tScreen, mainUITree, &uiMutex, true, &verticalOffset, &lastRenderedTreeLines)
	}
	drawStatusLine(tScreen, viewMode)
	tScreen.Show()
	prevViewMode = viewMode
}

// RunMainUI — основной цикл: дерево по умолчанию, переключение 1/2/3, выход q/Esc/Ctrl+C.
func RunMainUI(
	ctx context.Context,
	cancel context.CancelFunc,
	rootFolder string,
	updates <-chan FileChange,
) {
	absRoot, err := filepath.Abs(rootFolder)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка абсолютного пути: %v\n", err)
		os.Exit(1)
	}

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

	tScreen = screen
	changeLog = nil
	const changeLogMax = 2000

	repaintCh := make(chan struct{}, 1)
	mainUITree = tree
	colAnimator = newTreeColorAnimator(tree, &uiMutex, ctx, repaintCh)

	viewMode = appViewTree
	verticalOffset = 0
	prevViewMode = viewMode
	lastRenderedTreeLines = 0
	mainUIListView = newListJournalView()

	mainRedraw()

	events := make(chan tcell.Event, 16)
	go func() {
		for {
			ev := tScreen.PollEvent()
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
			uiMutex.Lock()
			var applied bool
			ch, applied, err := ApplyChange(tree, ch, absRoot)
			if err != nil {
				fmt.Fprintf(os.Stderr, "пересканирование каталога: %v\n", err)
			}
			if applied {
				changeLog = append(changeLog, ch)
				if len(changeLog) > changeLogMax {
					changeLog = changeLog[len(changeLog)-changeLogMax:]
				}
			}
			uiMutex.Unlock()
			colAnimator.Notify()

			mainRedraw()

		case <-repaintCh:
			mainRedraw()

		case ev := <-events:
			switch e := ev.(type) {
			case *tcell.EventResize:
				tScreen.Sync()
				mainRedraw()

			case *tcell.EventKey:
				if isQuitKey(e) {
					cancel()
					return
				}
				handleMainUIKeyEvent(e)
			}
		}
	}
}

// handleMainUIKeyEvent — прокрутка списка/дерева, переключение режимов 1/2/3. Выход обрабатывается в RunMainUI.
func handleMainUIKeyEvent(e *tcell.EventKey) {
	switch viewMode {
	case appViewList:
		uiMutex.RLock()
		n := len(changeLog)
		uiMutex.RUnlock()
		_, kh := tScreen.Size()
		cr := kh - 1
		switch e.Key() {
		case tcell.KeyUp:
			if mainUIListView.handleScrollKeyUp(n, cr) {
				mainRedraw()
			}
		case tcell.KeyDown:
			if mainUIListView.handleScrollKeyDown(n, cr) {
				mainRedraw()
			}
		case tcell.KeyPgUp:
			if mainUIListView.handleScrollPageUp(n, cr) {
				mainRedraw()
			}
		case tcell.KeyPgDn:
			if mainUIListView.handleScrollPageDown(n, cr) {
				mainRedraw()
			}
		}
	case appViewTree, appViewTreeDiff:
		switch e.Key() {
		case tcell.KeyUp:
			if verticalOffset > 0 {
				verticalOffset--
				mainRedraw()
			}
		case tcell.KeyDown:
			verticalOffset++
			mainRedraw()
		case tcell.KeyPgUp:
			_, kh := tScreen.Size()
			page := kh - 1
			if page > 0 && verticalOffset > 0 {
				verticalOffset -= page
				if verticalOffset < 0 {
					verticalOffset = 0
				}
				mainRedraw()
			}
		case tcell.KeyPgDn:
			_, kh := tScreen.Size()
			page := kh - 1
			if page > 0 {
				verticalOffset += page
				mainRedraw()
			}
		}
	}
	if e.Key() == tcell.KeyRune {
		switch e.Rune() {
		case '1':
			viewMode = appViewList
			mainRedraw()
		case '2':
			viewMode = appViewTree
			mainRedraw()
		case '3':
			viewMode = appViewTreeDiff
			mainRedraw()
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
		label = "[list] " + label
	case appViewTree:
		label = "[tree] " + label
	case appViewTreeDiff:
		label = "[tree diff] " + label
	}
	w, _ := screen.Size()
	style := tcell.StyleDefault.Reverse(true)
	for x := 0; x < w; x++ {
		screen.SetContent(x, y, ' ', nil, style)
	}
	drawString(screen, 0, y, label, style)
}
