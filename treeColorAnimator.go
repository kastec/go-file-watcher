package main

import (
	"context"
	"image/color"
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

func applyTreeColors(root *DiskItemInfo, now time.Time, blend time.Duration) (needRepaint bool) {
	return walkItems(&root.Items, now, blend)
}

func walkItems(items *[]*DiskItemInfo, now time.Time, blend time.Duration) (needRepaint bool) {
	for i := 0; i < len(*items); {
		node := (*items)[i]
		if walkItems(&node.Items, now, blend) {
			needRepaint = true
		}
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
			} else {
				start := startColorForNode(node)
				end := endColorForUpdatingNode(node)
				node.Color = calcColor(node.ChangeTime, now, start, end, blend)
				needRepaint = true
			}
		}
		i++
	}
	return needRepaint
}

// treeColorAnimator периодически обновляет поле Color у узлов с IsUpdating.
type treeColorAnimator struct {
	tree    *DiskItemInfo
	mu      *sync.RWMutex
	ctx     context.Context
	repaint chan<- struct{}

	running   atomic.Bool
	blendNano atomic.Int64 // time.Duration наносекунды; по умолчанию ChangingColorTime
}

func newTreeColorAnimator(tree *DiskItemInfo, mu *sync.RWMutex, ctx context.Context, repaint chan<- struct{}) *treeColorAnimator {
	a := &treeColorAnimator{
		tree:    tree,
		mu:      mu,
		ctx:     ctx,
		repaint: repaint,
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
			needRepaint := applyTreeColors(a.tree, now, a.blendDuration())
			//keepRunning := needRepaint || anyUpdating(a.tree)
			a.mu.Unlock()

			if needRepaint {
				select {
				case a.repaint <- struct{}{}:
				default:
				}
			}
			if !needRepaint {
				return
			}
			// if !keepRunning {
			// 	return
			// }
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
