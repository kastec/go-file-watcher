package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/gdamore/tcell/v2"

	"folder-monitor/utils"
)

// RunListUI выводит события изменений построчно в консоль (без tcell).
// Завершается при отмене ctx или при нажатии Ctrl+C.
func RunListUI(ctx context.Context, cancel context.CancelFunc, updates <-chan FileChange) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)

	fmt.Println("Мониторинг изменений. Для выхода нажмите Ctrl+C.")

	for {
		select {
		case <-ctx.Done():
			return

		case <-sigs:
			cancel()
			return

		case ch, ok := <-updates:
			if !ok {
				return
			}
			fmt.Println(formatChangeLine(ch))
		}
	}
}

// listScrollFollowLatest — показывать хвост журнала (последние строки).
const listScrollFollowLatest = -1

// listJournalView — прокрутка и отрисовка журнала в tcell-режиме списка.
type listJournalView struct {
	verticalOffsetLine int
}

func newListJournalView() *listJournalView {
	return &listJournalView{verticalOffsetLine: listScrollFollowLatest}
}

func (v *listJournalView) resetScroll() {
	v.verticalOffsetLine = listScrollFollowLatest
}

// handleScrollKeyUp обрабатывает «Вверх»; возвращает true, если нужна перерисовка.
func (v *listJournalView) handleScrollKeyUp(lineCount, contentRows int) bool {
	if contentRows <= 0 || lineCount <= contentRows {
		return false
	}
	tailStart := lineCount - contentRows
	if v.verticalOffsetLine == listScrollFollowLatest {
		if tailStart > 0 {
			v.verticalOffsetLine = tailStart - 1
			return true
		}
		return false
	}
	if v.verticalOffsetLine > 0 {
		v.verticalOffsetLine--
		return true
	}
	return false
}

// handleScrollKeyDown обрабатывает «Вниз»; возвращает true, если нужна перерисовка.
func (v *listJournalView) handleScrollKeyDown(lineCount, contentRows int) bool {
	if contentRows <= 0 || lineCount <= contentRows || v.verticalOffsetLine == listScrollFollowLatest {
		return false
	}
	tailStart := lineCount - contentRows
	v.verticalOffsetLine = utils.If(v.verticalOffsetLine >= tailStart, listScrollFollowLatest, v.verticalOffsetLine+1)
	return true
}

// handleScrollPageUp сдвигает первую видимую строку вверх на contentRows.
func (v *listJournalView) handleScrollPageUp(lineCount, contentRows int) bool {
	if contentRows <= 0 || lineCount <= contentRows {
		return false
	}
	tailStart := lineCount - contentRows
	if v.verticalOffsetLine == listScrollFollowLatest {
		newOff := tailStart - contentRows
		if newOff < 0 {
			newOff = 0
		}
		v.verticalOffsetLine = newOff
		return true
	}
	newOff := v.verticalOffsetLine - contentRows
	if newOff < 0 {
		newOff = 0
	}
	if newOff == v.verticalOffsetLine {
		return false
	}
	v.verticalOffsetLine = newOff
	return true
}

// handleScrollPageDown сдвигает вниз на contentRows; у нижней границы — режим хвоста.
func (v *listJournalView) handleScrollPageDown(lineCount, contentRows int) bool {
	if contentRows <= 0 || lineCount <= contentRows {
		return false
	}
	if v.verticalOffsetLine == listScrollFollowLatest {
		return false
	}
	tailStart := lineCount - contentRows
	newOff := v.verticalOffsetLine + contentRows

	v.verticalOffsetLine = utils.If(newOff >= tailStart, listScrollFollowLatest, newOff)

	return true
}

// renderChangeLog рисует журнал; расчёт первой строки и правки verticalOffsetLine — здесь.
func renderChangeLog(screen tcell.Screen, lines []string, view *listJournalView) {
	w, h := screen.Size()
	if w <= 0 || h <= 1 {
		return
	}
	contentRows := h - 1
	if contentRows <= 0 {
		return
	}
	if len(lines) == 0 {
		view.verticalOffsetLine = listScrollFollowLatest
		drawString(screen, 0, 0, "Пока нет событий изменений.", tcell.StyleDefault)
		return
	}
	tailStart := 0
	if len(lines) > contentRows {
		tailStart = len(lines) - contentRows
	}
	var start int
	if len(lines) <= contentRows {
		view.verticalOffsetLine = listScrollFollowLatest
		start = 0
	} else {
		off := view.verticalOffsetLine
		switch {
		case off == listScrollFollowLatest:
			start = tailStart
		case off+contentRows >= len(lines):
			start = tailStart
			view.verticalOffsetLine = listScrollFollowLatest
		case off > tailStart:
			start = tailStart
			view.verticalOffsetLine = listScrollFollowLatest
		case off < 0:
			start = 0
			view.verticalOffsetLine = 0
		default:
			start = off
		}
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

func formatChangeLine(ch FileChange) string {
	itemType := "DIR "
	sizePart := ""
	if ch.IsFile {
		itemType = "FILE"
		sizePart = " " + formatSize(ch.Size)
	}

	return fmt.Sprintf(
		"%s [%-8s] %s %s%s",
		ch.Time.Format("15:04:05.000"),
		ch.ChangeType.String(),
		itemType,
		ch.FullPath,
		sizePart,
	)
}
