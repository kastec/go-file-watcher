package main

import (
	"context"
	"os"
	"time"
)

// WaitForStableSize ждёт, пока размер файла по пути absPath стабилизируется:
// два последовательных опроса с интервалом interval возвращают одинаковый
// размер, и при этом файл не заблокирован на запись.
//
// Если файл заблокирован (os.OpenFile возвращает ошибку), счётчик
// стабильности сбрасывается, чтобы не принять совпадение размеров за
// завершение записи.
//
// Возвращает ошибку, если ctx отменён или файл исчез с диска.
func WaitForStableSize(ctx context.Context, absPath string, interval time.Duration) (int64, error) {
	var prev int64 = -1

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(interval):
		}

		// Проверяем, не заблокирован ли файл записывающим потоком.
		f, err := os.OpenFile(absPath, os.O_RDONLY, 0)
		if err != nil {
			// Файл недоступен для чтения — сбрасываем предыдущий размер,
			// чтобы не засчитать совпадение при следующей попытке.
			prev = -1
			continue
		}
		f.Close()

		info, err := os.Stat(absPath)
		if err != nil {
			return 0, err
		}

		current := info.Size()
		if current == prev {
			return current, nil
		}
		prev = current
	}
}
