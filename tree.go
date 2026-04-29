package main

import (
	"fmt"
	"hash/fnv"
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

// refreshDirChildren очищает Items у каталога по relPath и заново заполняет их с диска.
// absRoot — абсолютный корень наблюдения (тот же базовый путь, что у FileChange.FullPath).
func refreshDirChildren(root *DiskItemInfo, relPath, absRoot string) error {
	node := FindNode(root, relPath)
	if node == nil || node.IsFile {
		return nil
	}
	node.Items = nil
	dirAbs := filepath.Join(absRoot, filepath.FromSlash(relPath))
	return scanChildren(node, dirAbs)
}

// namesHash вычисляет порядко-независимый хеш набора имён файлов/каталогов.
// Хеширует каждое имя через FNV-64a и суммирует результаты.
// Возвращает 0 для пустого набора.
func namesHash(names []string) uint64 {
	var sum uint64
	for _, name := range names {
		h := fnv.New64a()
		h.Write([]byte(name))
		sum += h.Sum64()
	}
	return sum
}

// dirDiskHash читает одноуровневое содержимое каталога по absPath с диска
// и возвращает его namesHash. Возвращает 0 при ошибке или пустом каталоге.
func dirDiskHash(absPath string) uint64 {
	entries, err := os.ReadDir(absPath)
	if err != nil || len(entries) == 0 {
		return 0
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return namesHash(names)
}

// dirNodeHash вычисляет namesHash содержимого каталога по его Items в дереве.
// Возвращает 0 для каталога без детей.
func dirNodeHash(node *DiskItemInfo) uint64 {
	if len(node.Items) == 0 {
		return 0
	}
	names := make([]string, len(node.Items))
	for i, item := range node.Items {
		names[i] = item.Name
	}
	return namesHash(names)
}

// resetSubtreeChangeType рекурсивно сбрасывает ChangeType и анимацию
// у всех потомков node. Используется при обнаружении переименования каталога,
// чтобы его дети не продолжали анимироваться как удалённые.
func resetSubtreeChangeType(node *DiskItemInfo) {
	for _, child := range node.Items {
		child.ChangeType = None
		child.IsUpdating = false
		child.Color = defaultColorForNode(child)
		resetSubtreeChangeType(child)
	}
}

// tryApplyAsRename проверяет, не является ли Created-каталог переименованием
// существующего каталога в том же родителе. Сравнивает хеш содержимого
// нового каталога (с диска) с хешами Items соседних каталогов в дереве.
// Если найдено совпадение — переименовывает узел на месте и возвращает true.
// Пустые каталоги (hash == 0) не проверяются: слишком высок риск ложных совпадений.
func tryApplyAsRename(root *DiskItemInfo, ch FileChange, absRoot string) bool {
	absPath := filepath.Join(absRoot, filepath.FromSlash(ch.FullPath))
	newHash := dirDiskHash(absPath)
	if newHash == 0 {
		return false
	}

	parent, newName := findParent(root, ch.FullPath)
	if parent == nil {
		return false
	}

	for _, sibling := range parent.Items {
		if sibling.IsFile || sibling.Name == newName {
			continue
		}
		nodeHash := dirNodeHash(sibling)
		AppendFileLine("log.txt", fmt.Sprintf("nodeHash: %d, newHash: %d", nodeHash, newHash))
		if nodeHash == newHash {
			// Нашли старый каталог с тем же содержимым — это переименование.
			sibling.Name = newName
			sibling.ChangeType = Created
			sibling.ChangeTime = ch.Time
			sibling.IsUpdating = true
			sibling.Color = changedFileColor
			resetSubtreeChangeType(sibling)
			return true
		}
	}
	return false
}

// tryApplyFileRename проверяет, не является ли Renamed-файл переименованием
// существующего файла в том же родителе. Ищет среди соседей узел с IsFile=true,
// ChangeType=Removed и совпадающим размером. Если найден — переименовывает
// его на месте, устанавливает ChangeType=Modified и возвращает true.
func tryApplyFileRename(root *DiskItemInfo, ch FileChange) bool {
	parent, newName := findParent(root, ch.FullPath)
	if parent == nil {
		return false
	}

	for _, sibling := range parent.Items {
		if !sibling.IsFile || sibling.Name == newName {
			continue
		}
		if sibling.ChangeType == Removed && sibling.Size == ch.Size {
			sibling.Name = newName
			sibling.Size = ch.Size
			sibling.ChangeType = Modified
			sibling.ChangeTime = ch.Time
			sibling.IsUpdating = true
			sibling.Color = changedFileColor
			return true
		}
	}
	return false
}

// AppendLine добавляет строку в конец файла
func AppendFileLine(filename string, line string) error {
	// Открываем файл:
	// O_APPEND — добавлять в конец
	// O_CREATE — создать, если не существует
	// O_WRONLY — только для записи
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Добавляем строку и символ переноса строки
	if _, err := f.WriteString(line + "\n"); err != nil {
		return err
	}

	return nil
}

// ApplyChange применяет одно событие изменения к дереву.
// Возвращает (возможно скорректированное) событие и признак, было ли оно применено.
//
//   - Created / Modified → добавить или обновить узел
//   - Removed            → пометить узел (анимация), фактическое удаление
//     из дерева после интервала анимации цвета в treeColorAnimator (ChangingColorTime или ChangingColorTimeDiff)
	//   - Renamed            → для каталога: сопоставить по хешу содержимого с соседом
	//     и переименовать на месте; для файла: сопоставить по размеру с Removed-соседом
	//     и переименовать на месте (ChangeType=Modified)
func ApplyChange(root *DiskItemInfo, ch FileChange, absRoot string) (FileChange, bool) {
	node := FindNode(root, ch.FullPath)

	// Если узел уже помечен как Removed, игнорируем любые последующие
	// события для этого же пути, чтобы не "оживлять" удалённый файл.
	if node != nil && node.ChangeType == Removed {
		return ch, false
	}

	// Для Removed в watcher IsFile не заполняется (файла уже нет на диске).
	// Берём признак из дерева, иначе каталог может ошибочно отображаться как файл.
	if ch.ChangeType == Removed && node != nil {
		ch.IsFile = node.IsFile
	}
	AppendFileLine("./log.txt", fmt.Sprint("ch: ", ch.Name, "ch.ChangeType:", ch.ChangeType))
	switch ch.ChangeType {
	case Created, Modified:
		AddNode(root, ch)

	case Removed:
		if node == nil {
			return ch, false
		}
		node.ChangeType = Removed
		node.ChangeTime = ch.Time
		node.IsUpdating = true
		node.Color = removedColor

	case Renamed:
		// fswatcher на Windows присылает EventRename на старый путь (удалить)
		// и EventRename на новый путь (добавить). Сюда приходит уже новый путь.
		// Для каталога: пытаемся найти совпадение по хешу содержимого среди соседей
		// и переименовать узел на месте (сохраняет вложенные файлы).
		// Для файла: пытаемся найти Removed-сосед с тем же размером и переименовать.
		if !ch.IsFile && absRoot != "" {
			if tryApplyAsRename(root, ch, absRoot) {
				break
			}
			// Совпадение не найдено — добавляем как новый каталог с перечитыванием.
			AddNode(root, ch)
			if err := refreshDirChildren(root, ch.FullPath, absRoot); err != nil {
				return ch, true
			}
		} else {
			if !tryApplyFileRename(root, ch) {
				AddNode(root, ch)
			}
		}

	case None:
		// нечего делать
	}

	return ch, true
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
