// Package sqlutil 提供組合 SQL 查詢時共用的小型輔助函式。
package sqlutil

import "strings"

// Placeholders 回傳 n 個以逗號分隔的 `?` 佔位符，例如 n=3 時為 "?,?,?"；n<=0 時回傳空字串。
func Placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// Args 將任意型別的切片轉為 database/sql 所需的 []any 參數。
func Args[T any](list []T) []any {
	args := make([]any, len(list))
	for i, v := range list {
		args[i] = v
	}
	return args
}

// Chunk 將切片依 size 分段；回傳的每一段共用原切片的底層陣列，不複製元素。
func Chunk[T any](list []T, size int) [][]T {
	var out [][]T
	for len(list) > 0 {
		n := min(size, len(list))
		out = append(out, list[:n])
		list = list[n:]
	}
	return out
}
