package parallel

// ShouldReportProgress reports whether to emit a progress line for index out of total
// (1-based index). Reports the first, last, and periodic updates (every 25 for large
// batches, midpoint for small batches).
func ShouldReportProgress(index, total int) bool {
	if total <= 1 {
		return false
	}
	if index == 1 || index == total {
		return true
	}
	if total <= 10 {
		return index == (total+1)/2
	}
	return index%25 == 0
}
