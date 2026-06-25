package output

// sanitizeCSVField escapes spreadsheet formula injection for CSV output.
// Fields beginning with =, +, -, or @ (after optional leading tab/CR/LF that
// Excel ignores) are prefixed with a single quote so Excel and similar tools
// treat them as plain text.
func sanitizeCSVField(s string) string {
	if s == "" {
		return s
	}
	i := 0
	for i < len(s) && (s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
		i++
	}
	if i >= len(s) {
		return s
	}
	switch s[i] {
	case '=', '+', '-', '@':
		return "'" + s
	default:
		return s
	}
}
