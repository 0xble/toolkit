package schemacache

// SizeForTest observes cleanup without adding a production or public API.
func SizeForTest() int {
	n := 0
	cache.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}
