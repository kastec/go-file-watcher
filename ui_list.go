package main

import (
	"context"
	"fmt"
	"image/color"
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

func tcellStyleFromColor(c color.Color) tcell.Style {
	r16, g16, b16, _ := c.RGBA()
	return tcell.StyleDefault.Foreground(
		tcell.NewRGBColor(int32(r16>>8), int32(g16>>8), int32(b16>>8)),
	)
}

// changeLogMainStyle — как базовый itemStyle дерева: удалён, файл, каталог.
func changeLogMainStyle(ch FileChange) tcell.Style {
	if ch.ChangeType == Removed {
		return tcellStyleFromColor(removedColor)
	}
	if ch.IsFile {
		return tcellStyleFromColor(defaultFileTextColor)
	}
	return tcellStyleFromColor(defaultDirTextColor)
}

func changeLogPathSegment(ch FileChange) string {
	if ch.IsFile {
		return ch.FullPath
	}
	dirPath := ch.FullPath
	if dirPath == "" {
		dirPath = ch.Name
	}
	return fmt.Sprintf("[%s]", dirPath)
}

// drawChangeLogLineClipped рисует одну запись журнала с теми же цветами, что и дерево.
func drawChangeLogLineClipped(screen tcell.Screen, x, y, maxW int, ch FileChange) {
	sw, _ := screen.Size()
	right := x + maxW
	if right > sw {
		right = sw
	}
	timeStr := ch.Time.Format("15:04:05.000")
	meta := fmt.Sprintf(" [%-8s] ", ch.ChangeType.String())
	mainStyle := changeLogMainStyle(ch)
	metaStyle := tcell.StyleDefault
	switch ch.ChangeType {
	case Removed:
		metaStyle = tcellStyleFromColor(removedColor)
	case Created:
		metaStyle = tcellStyleFromColor(changedFileColor)
	}
	col := x
	drawSeg := func(s string, st tcell.Style) {
		if col >= right {
			return
		}
		budget := right - col
		if budget > len(s) {
			screen.PutStrStyled(col, y, s, st)
			col += len(s)
		} else {
			rs := []rune(s)
			clipped := string(rs[:budget])
			screen.PutStrStyled(col, y, clipped, st)
			col += runeLen(clipped)
		}
	}

	drawSeg(timeStr, fileSizeStyle())
	drawSeg(meta, metaStyle)
	drawSeg(changeLogPathSegment(ch), mainStyle)
	if ch.IsFile && !(ch.ChangeType == Removed && ch.Size == 0) {
		drawSeg(" ", mainStyle)
		drawSeg(formatSize(ch.Size), fileSizeStyle())
	}
}

// renderChangeLog рисует журнал; расчёт первой строки и правки verticalOffsetLine — здесь.
func renderChangeLog(screen tcell.Screen, lines []FileChange, view *listJournalView) {
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
	for i, ch := range lines[start:] {
		if i >= contentRows {
			break
		}
		drawChangeLogLineClipped(screen, 0, i, w, ch)
	}
}

func formatChangeLine(ch FileChange) string {
	sizePart := ""
	if ch.IsFile && !(ch.ChangeType == Removed && ch.Size == 0) {
		sizePart = " " + formatSize(ch.Size)
	}
	return fmt.Sprintf(
		"%s [%-8s] %s%s",
		ch.Time.Format("15:04:05.000"),
		ch.ChangeType.String(),
		changeLogPathSegment(ch),
		sizePart,
	)
}
