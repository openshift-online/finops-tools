package output

// sanitizeCSVField escapes spreadsheet formula injection for CSV output.
// Fields beginning with =, +, -, or @ are prefixed with a single quote so
// Excel and similar tools treat them as plain text.
func sanitizeCSVField(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@':
		return "'" + s
	default:
		return s
	}
}
