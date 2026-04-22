package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

const defaultRootFolder = "d:/0/1"

func main() {
	var rootFolder string
	flag.StringVar(&rootFolder, "path", defaultRootFolder, "каталог для мониторинга")
	flag.Parse()

	info, err := os.Stat(rootFolder)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: не удалось открыть путь %q: %v\n", rootFolder, err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "ошибка: %q не является каталогом\n", rootFolder)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updates := make(chan FileChange, 64)

	go func() {
		if err := RunWatcher(ctx, rootFolder, updates); err != nil {
			fmt.Fprintf(os.Stderr, "ошибка watcher: %v\n", err)
			cancel()
		}
	}()

	RunMainUI(ctx, cancel, rootFolder, updates)
}
