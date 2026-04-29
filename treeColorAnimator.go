package main

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

func defaultColorForNode(node *DiskItemInfo) color.Color {
	if node.IsFile {
		return defaultFileTextColor
	}
	return defaultDirTextColor
}

func startColorForNode(node *DiskItemInfo) color.Color {
	if node.ChangeType == Removed {
		return removedColor
	}
	// Created / Modified / Renamed
	return changedFileColor
}

func endColorForUpdatingNode(node *DiskItemInfo) color.Color {
	if node.ChangeType == Removed {
		return removedFadeTargetColor
	}
	return defaultColorForNode(node)
}

func applyTreeColors(root *DiskItemInfo, now time.Time, blend time.Duration, absRoot string) (needRepaint bool, missing []FileChange) {
	return walkItems(&root.Items, "", now, blend, absRoot)
}

func walkItems(items *[]*DiskItemInfo, relPathPrefix string, now time.Time, blend time.Duration, absRoot string) (needRepaint bool, missing []FileChange) {
	for i := 0; i < len(*items); {
		node := (*items)[i]
		childRepaint, childMissing := walkItems(&node.Items, relPathPrefix+node.Name+"/", now, blend, absRoot)
		if childRepaint {
			needRepaint = true
		}
		missing = append(missing, childMissing...)

		if node.IsUpdating {
			elapsed := now.Sub(node.ChangeTime)
			if elapsed >= blend {
				if node.ChangeType == Removed {
					*items = append((*items)[:i], (*items)[i+1:]...)
					needRepaint = true
					continue
				}
				node.IsUpdating = false
				node.Color = defaultColorForNode(node)
				needRepaint = true

				// После завершения анимации проверяем наличие элемента на диске.
				// Если элемент исчез — формируем событие удаления.
				if absRoot != "" {
					relPath := relPathPrefix + node.Name
					absPath := filepath.Join(absRoot, filepath.FromSlash(relPath))
					if _, err := os.Stat(absPath); os.IsNotExist(err) {
						missing = append(missing, FileChange{
							Time:       now,
							ChangeType: Removed,
							IsFile:     node.IsFile,
							Name:       node.Name,
							FullPath:   relPath,
						})
					}
				}
			} else {
				start := startColorForNode(node)
				end := endColorForUpdatingNode(node)
				node.Color = calcColor(node.ChangeTime, now, start, end, blend)
				needRepaint = true
			}
		}
		i++
	}
	return
}

// treeColorAnimator периодически обновляет поле Color у узлов с IsUpdating.
type treeColorAnimator struct {
	tree    *DiskItemInfo
	mu      *sync.RWMutex
	ctx     context.Context
	repaint chan<- struct{}
	absRoot string
	missing chan<- FileChange

	running   atomic.Bool
	blendNano atomic.Int64 // time.Duration наносекунды; по умолчанию ChangingColorTime
}

func newTreeColorAnimator(tree *DiskItemInfo, mu *sync.RWMutex, ctx context.Context, repaint chan<- struct{}, absRoot string, missing chan<- FileChange) *treeColorAnimator {
	a := &treeColorAnimator{
		tree:    tree,
		mu:      mu,
		ctx:     ctx,
		repaint: repaint,
		absRoot: absRoot,
		missing: missing,
	}
	a.blendNano.Store(int64(ChangingColorTime))
	return a
}

// SetBlendDuration задаёт длительность перехода цвета (например ChangingColorTimeDiff в режиме Diff).
func (a *treeColorAnimator) SetBlendDuration(d time.Duration) {
	a.blendNano.Store(int64(d))
}

func (a *treeColorAnimator) blendDuration() time.Duration {
	return time.Duration(a.blendNano.Load())
}

// Notify запускает фоновый цикл обновления цветов, если он ещё не выполняется.
func (a *treeColorAnimator) Notify() {
	if !a.running.CompareAndSwap(false, true) {
		return
	}
	go a.run()
}

func (a *treeColorAnimator) run() {
	defer a.running.Store(false)

	ticker := time.NewTicker(UpdateTreePeriod)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.mu.Lock()
			now := time.Now()
			needRepaint, missing := applyTreeColors(a.tree, now, a.blendDuration(), a.absRoot)
			a.mu.Unlock()

			for _, fc := range missing {
				if a.missing != nil {
					select {
					case a.missing <- fc:
					default:
					}
				}
			}

			if needRepaint {
				select {
				case a.repaint <- struct{}{}:
				default:
				}
			}
			if !needRepaint {
				return
			}
		}
	}
}

// calcColor линейно интерполирует цвет от start к end по времени с ChangeTime до now на интервале blend.
func calcColor(changeTime, now time.Time, start, end color.Color, blend time.Duration) color.Color {
	d := now.Sub(changeTime)
	t := float64(d) / float64(blend)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	sr, sg, sb, _ := start.RGBA()
	er, eg, eb, _ := end.RGBA()
	srf := float64(sr >> 8)
	sgf := float64(sg >> 8)
	sbf := float64(sb >> 8)
	erf := float64(er >> 8)
	egf := float64(eg >> 8)
	ebf := float64(eb >> 8)
	return color.NRGBA{
		R: uint8(srf + t*(erf-srf) + 0.5),
		G: uint8(sgf + t*(egf-sgf) + 0.5),
		B: uint8(sbf + t*(ebf-sbf) + 0.5),
		A: 0xff,
	}
}
