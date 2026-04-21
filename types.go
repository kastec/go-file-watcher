package main

import (
	"image/color"
	"time"
)

// FileChangeType описывает тип изменения файла или каталога.
type FileChangeType int

const (
	None     FileChangeType = iota // 0 — нет изменений
	Created                        // 1 — создан
	Modified                       // 2 — изменён
	Renamed                        // 3 — переименован
	Removed                        // 4 — удалён
)

func (t FileChangeType) String() string {
	switch t {
	case Created:
		return "Created"
	case Modified:
		return "Modified"
	case Renamed:
		return "Renamed"
	case Removed:
		return "Removed"
	default:
		return "None"
	}
}

// FileChange описывает одно событие изменения файла или каталога.
// FullPath — путь относительно RootFolder.
type FileChange struct {
	Time       time.Time
	ChangeType FileChangeType
	IsFile     bool
	Name       string
	Size       int64
	FullPath   string
}

// DiskItemInfo — узел иерархического дерева файловой системы в памяти.
type DiskItemInfo struct {
	Name       string
	IsFile     bool
	Size       int64
	Items      []*DiskItemInfo
	ChangeType FileChangeType
	ChangeTime time.Time
	IsUpdating bool
	Color      color.Color
}
