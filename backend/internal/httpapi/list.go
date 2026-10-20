package httpapi

// listDTO — единый формат всех списков API: {"items": [...], "next_cursor": "..." | null}.
// next_cursor = null — страница последняя; короткие списки (тарифы, группы, сотрудники)
// пока отдаются целиком, и next_cursor у них всегда null.
type listDTO[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// listOf — весь список одной страницей.
func listOf[T any](items []T) listDTO[T] {
	if items == nil {
		items = []T{}
	}
	return listDTO[T]{Items: items}
}

// pageOf — страница списка; nextCursor == "" — следующей страницы нет.
func pageOf[T any](items []T, nextCursor string) listDTO[T] {
	l := listOf(items)
	if nextCursor != "" {
		l.NextCursor = &nextCursor
	}
	return l
}
