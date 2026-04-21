package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ScanDir рекурсивно сканирует каталог rootPath и строит дерево DiskItemInfo.
// Корневой узел соответствует самому rootPath.
func ScanDir(rootPath string) (*DiskItemInfo, error) {
	info, err := os.Stat(rootPath)
	if err != nil {
		return nil, err
	}

	root := &DiskItemInfo{
		Name:   info.Name(),
		IsFile: false,
		Color:  defaultDirTextColor,
	}

	if err := scanChildren(root, rootPath); err != nil {
		return nil, err
	}

	return root, nil
}

// scanChildren заполняет Items узла, рекурсивно обходя каталог dirPath.
func scanChildren(node *DiskItemInfo, dirPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		c := defaultDirTextColor
		if !entry.IsDir() {
			c = defaultFileTextColor
		}
		child := &DiskItemInfo{
			Name:   entry.Name(),
			IsFile: !entry.IsDir(),
			Color:  c,
		}

		if entry.IsDir() {
			if err := scanChildren(child, filepath.Join(dirPath, entry.Name())); err != nil {
				return err
			}
		} else {
			if fi, err := entry.Info(); err == nil {
				child.Size = fi.Size()
			}
		}

		node.Items = append(node.Items, child)
	}

	return nil
}

// FindNode ищет узел по relPath (пути относительно корня дерева, разделители — / или \).
// Возвращает nil, если узел не найден.
func FindNode(root *DiskItemInfo, relPath string) *DiskItemInfo {
	parts := splitPath(relPath)
	if len(parts) == 0 {
		return root
	}

	current := root
	for _, part := range parts {
		found := findChild(current, part)
		if found == nil {
			return nil
		}
		current = found
	}

	return current
}

// findParent возвращает родительский узел и имя последнего сегмента пути.
// Если путь состоит из одного сегмента — родитель это root.
func findParent(root *DiskItemInfo, relPath string) (*DiskItemInfo, string) {
	parts := splitPath(relPath)
	if len(parts) == 0 {
		return nil, ""
	}

	if len(parts) == 1 {
		return root, parts[0]
	}

	parent := FindNode(root, strings.Join(parts[:len(parts)-1], "/"))
	return parent, parts[len(parts)-1]
}

// AddNode добавляет или обновляет узел в дереве согласно change.
// Если промежуточные каталоги отсутствуют, они создаются автоматически.
func AddNode(root *DiskItemInfo, change FileChange) {
	parts := splitPath(change.FullPath)
	if len(parts) == 0 {
		return
	}

	current := root
	for i, part := range parts {
		isLast := i == len(parts)-1
		child := findChild(current, part)

		if child == nil {
			c := defaultDirTextColor
			if isLast && change.IsFile {
				c = defaultFileTextColor
			}
			child = &DiskItemInfo{
				Name:  part,
				Color: c,
			}
			current.Items = append(current.Items, child)
		}

		if isLast {
			child.IsFile = change.IsFile
			child.Size = change.Size
			child.ChangeType = change.ChangeType
			child.ChangeTime = change.Time
			switch change.ChangeType {
			case Created, Modified, Renamed:
				child.IsUpdating = true
				// Созданный/изменённый — стартовый цвет.
				child.Color = changedFileColor
			default:
				// None / Removed не должны попадать в AddNode с конечным узлом.
			}
		} else {
			child.IsFile = false
			if child.Color == nil {
				child.Color = defaultDirTextColor
			}
		}

		current = child
	}
}

// RemoveNode удаляет узел по relPath из дерева.
// Если узел не найден, ничего не происходит.
func RemoveNode(root *DiskItemInfo, relPath string) {
	parent, name := findParent(root, relPath)
	if parent == nil || name == "" {
		return
	}

	for i, child := range parent.Items {
		if child.Name == name {
			parent.Items = append(parent.Items[:i], parent.Items[i+1:]...)
			return
		}
	}
}

// ApplyChange применяет одно событие изменения к дереву.
//
//   - Created / Modified → добавить или обновить узел
//   - Removed            → пометить узел (анимация), фактическое удаление
//     из дерева после ChangingColorTime в treeColorAnimator
//   - Renamed            → удалить старый узел (FullPath уже новый путь),
//     добавить новый с ChangeType = Renamed
func ApplyChange(root *DiskItemInfo, ch FileChange) {
	switch ch.ChangeType {
	case Created, Modified:
		AddNode(root, ch)

	case Removed:
		node := FindNode(root, ch.FullPath)
		if node == nil {
			return
		}
		node.ChangeType = Removed
		node.ChangeTime = ch.Time
		node.IsUpdating = true
		node.Color = removedColor

	case Renamed:
		// fswatcher на Windows присылает EventRename на старый путь (удалить)
		// и EventRename на новый путь (добавить). Сюда приходит уже новый путь
		// с ChangeType=Renamed — просто добавляем узел.
		// Удаление старого узла выполняется снаружи (в watcher.go) перед вызовом
		// ApplyChange с Removed.
		AddNode(root, ch)

	case None:
		// нечего делать
	}
}

// UpdateChangeType обновляет ChangeType и ChangeTime существующего узла.
func UpdateChangeType(root *DiskItemInfo, relPath string, ct FileChangeType, t time.Time) {
	node := FindNode(root, relPath)
	if node == nil {
		return
	}
	node.ChangeType = ct
	node.ChangeTime = t
}

// --- вспомогательные функции ---

// findChild ищет прямого потомка по имени.
func findChild(node *DiskItemInfo, name string) *DiskItemInfo {
	for _, child := range node.Items {
		if child.Name == name {
			return child
		}
	}
	return nil
}

// splitPath разбивает путь на сегменты, убирая пустые части и ведущие разделители.
func splitPath(p string) []string {
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return nil
	}

	var parts []string
	for _, seg := range strings.Split(p, "/") {
		if seg != "" && seg != "." {
			parts = append(parts, seg)
		}
	}

	return parts
}
