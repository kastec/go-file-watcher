package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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

func formatChangeLine(ch FileChange) string {
	itemType := "DIR "
	sizePart := ""
	if ch.IsFile {
		itemType = "FILE"
		sizePart = " " + formatSize(ch.Size)
	}

	return fmt.Sprintf(
		"%s [%-8s] %s %s%s",
		ch.Time.Format("15:04:05"),
		ch.ChangeType.String(),
		itemType,
		ch.FullPath,
		sizePart,
	)
}
