package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sgtdi/fswatcher"
)

const watcherCooldown = 1000 * time.Millisecond

// RunWatcher запускает мониторинг каталога rootFolder.
// Конвертирует fswatcher.WatchEvent в FileChange и отправляет в канал out.
// Не обращается к дереву — только формирует события изменений.
// Блокируется до отмены ctx.
func RunWatcher(
	ctx context.Context,
	rootFolder string,
	out chan<- FileChange,
) error {
	absRoot, err := filepath.Abs(rootFolder)
	if err != nil {
		return err
	}

	fsw, err := fswatcher.New(
		fswatcher.WithPath(absRoot),
		fswatcher.WithCooldown(watcherCooldown),
	)
	if err != nil {
		return err
	}

	go func() {
		if err := fsw.Watch(ctx); err != nil && err != context.Canceled {
			_ = err
		}
	}()

	for {
		select {
		case <-ctx.Done():
			fsw.Close()
			return nil

		case event, ok := <-fsw.Events():
			if !ok {
				return nil
			}

			for _, ch := range convertEvent(event, absRoot) {
				select {
				case out <- ch:
				case <-ctx.Done():
					fsw.Close()
					return nil
				}
			}
		}
	}
}

// convertEvent преобразует fswatcher.WatchEvent в один или два FileChange.
//
// Для EventRename на Windows fswatcher присылает два отдельных события:
//   - старый путь (файл уже не существует) → Removed
//   - новый путь  (файл существует на диске) → Renamed
//
// Различаем их через os.Stat: существует → Renamed, нет → Removed.
// Для всех остальных типов возвращается ровно один FileChange.
// fullPath в FileChange — путь относительно absRoot.
func convertEvent(event fswatcher.WatchEvent, absRoot string) []FileChange {
	relPath := toRelPath(event.Path, absRoot)
	if relPath == "" {
		return nil
	}

	ct := pickChangeType(event.Types)
	if ct == None {
		return nil
	}

	ch := FileChange{
		Time:     event.Time,
		FullPath: relPath,
		Name:     filepath.Base(relPath),
	}

	switch ct {
	case Renamed:
		// Определяем тип по наличию файла на диске.
		// Старый путь (уже удалён) → Removed.
		// Новый путь (существует)   → Renamed + метаданные.
		if _, err := os.Stat(event.Path); err != nil {
			ch.ChangeType = Removed
		} else {
			ch.ChangeType = Renamed
			fillFromDisk(&ch, event.Path)
		}

	case Removed:
		ch.ChangeType = Removed

	default: // Created, Modified
		ch.ChangeType = ct
		fillFromDisk(&ch, event.Path)
	}

	return []FileChange{ch}
}

// pickChangeType выбирает наиболее значимый тип из набора событий fswatcher.
// Приоритет: Remove > Rename > Create > Mod > остальные.
func pickChangeType(types []fswatcher.EventType) FileChangeType {
	result := None
	for _, t := range types {
		if candidate := mapEventType(t); priority(candidate) > priority(result) {
			result = candidate
		}
	}
	return result
}

func mapEventType(t fswatcher.EventType) FileChangeType {
	switch t {
	case fswatcher.EventCreate:
		return Created
	case fswatcher.EventMod:
		return Modified
	case fswatcher.EventRename:
		return Renamed
	case fswatcher.EventRemove:
		return Removed
	default:
		return None
	}
}

func priority(ct FileChangeType) int {
	switch ct {
	case Removed:
		return 4
	case Renamed:
		return 3
	case Created:
		return 2
	case Modified:
		return 1
	default:
		return 0
	}
}

// fillFromDisk заполняет IsFile и Size через os.Stat.
func fillFromDisk(ch *FileChange, absPath string) {
	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			ch.ChangeType = Removed
		}
		return
	}

	ch.IsFile = !info.IsDir()

	if ch.IsFile {
		ch.Size = info.Size()
	}
}

// toRelPath возвращает путь event.Path относительно absRoot.
// Возвращает пустую строку, если путь не принадлежит absRoot.
func toRelPath(absPath, absRoot string) string {
	absPath = filepath.ToSlash(absPath)
	absRoot = filepath.ToSlash(absRoot)

	prefix := strings.TrimSuffix(absRoot, "/") + "/"
	if !strings.HasPrefix(absPath, prefix) {
		return ""
	}
	return strings.TrimPrefix(absPath, prefix)
}
