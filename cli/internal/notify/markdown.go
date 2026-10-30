package notify

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

// placeholderSentinelOpen and placeholderSentinelClose wrap a placeholder name
// so CommonMark parsing cannot treat the substituted value as markup. They are
// private-use characters that do not appear in template text.
const (
	placeholderSentinelOpen  = "\uE000"
	placeholderSentinelClose = "\uE001"
)

const templateLinkStyle = `color:#0969da`

// renderedTemplate is a template field rendered for the HTML and text bodies.
type renderedTemplate struct {
	HTML  string
	Plain string
}

var templateMarkdown = goldmark.New(
	goldmark.WithRendererOptions(goldhtml.WithHardWraps()),
)

var (
	paragraphGap = regexp.MustCompile(`</p>\s*<p>`)
	listItemGap  = regexp.MustCompile(`</li>\s*<li>`)
	blockTagRE   = regexp.MustCompile(`</?(?:ul|ol|li|blockquote|pre|hr|h[1-6])\b[^>]*>`)
)

// renderTemplateMarkdown renders src as CommonMark. inline unwraps a single
// paragraph so the fragment can sit inside an existing heading or list item.
// Placeholder values are substituted after parsing, so characters in those
// values are literal text. Raw HTML is escaped. Links are limited to http,
// https, and mailto. Images are replaced with their alt text.
func renderTemplateMarkdown(src string, values map[string]string, inline bool) renderedTemplate {
	if strings.TrimSpace(src) == "" {
		return renderedTemplate{}
	}
	protected := protectPlaceholders(src, values)
	source := []byte(protected)
	doc := templateMarkdown.Parser().Parse(text.NewReader(source))
	prepareTemplateAST(doc, source, values)

	var buf bytes.Buffer
	if err := templateMarkdown.Renderer().Render(&buf, source, doc); err != nil {
		plain := strings.TrimSpace(restorePlaceholders(protected, values, false))
		return renderedTemplate{HTML: htmlEscape(plain), Plain: plain}
	}
	htmlFrag := strings.TrimSpace(styleTemplateLinks(restorePlaceholders(buf.String(), values, true)))
	if inline {
		htmlFrag = inlineHTMLFragment(htmlFrag)
	}
	plain := strings.TrimSpace(restorePlaceholders(renderPlain(doc, source), values, false))
	return renderedTemplate{HTML: htmlFrag, Plain: plain}
}

func protectPlaceholders(src string, values map[string]string) string {
	if len(values) == 0 || !strings.Contains(src, "{") {
		return src
	}
	oldnew := make([]string, 0, len(values)*2)
	for k := range values {
		oldnew = append(oldnew, "{"+k+"}", placeholderSentinelOpen+k+placeholderSentinelClose)
	}
	return strings.NewReplacer(oldnew...).Replace(src)
}

func restorePlaceholders(s string, values map[string]string, escape bool) string {
	if len(values) == 0 || !strings.Contains(s, placeholderSentinelOpen) {
		return s
	}
	oldnew := make([]string, 0, len(values)*2)
	for k, v := range values {
		if escape {
			v = htmlEscape(v)
		}
		oldnew = append(oldnew, placeholderSentinelOpen+k+placeholderSentinelClose, v)
	}
	return strings.NewReplacer(oldnew...).Replace(s)
}

func expandSentinelBytes(b []byte, values map[string]string) []byte {
	if len(b) == 0 || len(values) == 0 || !bytes.Contains(b, []byte(placeholderSentinelOpen)) {
		return b
	}
	return []byte(restorePlaceholders(string(b), values, false))
}

func styleTemplateLinks(fragment string) string {
	return strings.ReplaceAll(fragment, `<a href="`, `<a style="`+templateLinkStyle+`" href="`)
}

func singleParagraphInner(fragment string) (string, bool) {
	s := strings.TrimSpace(fragment)
	if strings.HasPrefix(s, "<p>") && strings.HasSuffix(s, "</p>") &&
		strings.Count(s, "<p>") == 1 && strings.Count(s, "</p>") == 1 {
		return s[len("<p>") : len(s)-len("</p>")], true
	}
	return "", false
}

// inlineHTMLFragment turns block Markdown into inline HTML: one paragraph is
// unwrapped, and further paragraphs or list items become line breaks.
func inlineHTMLFragment(fragment string) string {
	s := strings.TrimSpace(fragment)
	if inner, ok := singleParagraphInner(s); ok {
		return inner
	}
	s = paragraphGap.ReplaceAllString(s, "<br>")
	s = strings.ReplaceAll(s, "<p>", "")
	s = strings.ReplaceAll(s, "</p>", "")
	s = listItemGap.ReplaceAllString(s, "<br>")
	s = blockTagRE.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func prepareTemplateAST(doc ast.Node, source []byte, values map[string]string) {
	var (
		autos  []*ast.AutoLink
		links  []*ast.Link
		images []*ast.Image
		raws   []*ast.RawHTML
		blocks []*ast.HTMLBlock
	)
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.AutoLink:
			autos = append(autos, n)
		case *ast.Link:
			links = append(links, n)
		case *ast.Image:
			images = append(images, n)
		case *ast.RawHTML:
			raws = append(raws, n)
		case *ast.HTMLBlock:
			blocks = append(blocks, n)
		}
		return ast.WalkContinue, nil
	})
	for _, n := range autos {
		rewriteAutoLink(n, source, values)
	}
	for _, n := range links {
		if n.Parent() == nil {
			continue
		}
		n.Destination = expandSentinelBytes(n.Destination, values)
		if !safeLinkURL(string(n.Destination)) {
			liftChildren(n)
		}
	}
	for _, n := range images {
		liftChildren(n)
	}
	for _, n := range raws {
		if n.Segments == nil {
			continue
		}
		replaceWithLiteral(n, n.Segments.Value(source))
	}
	for _, n := range blocks {
		replaceHTMLBlock(n, source)
	}
}

func rewriteAutoLink(n *ast.AutoLink, source []byte, values map[string]string) {
	parent := n.Parent()
	if parent == nil {
		return
	}
	label := append([]byte(nil), n.Label(source)...)
	expanded := expandSentinelBytes([]byte(autoLinkURL(n, source)), values)
	needsRewrite := bytes.Contains(n.URL(source), []byte(placeholderSentinelOpen)) ||
		bytes.Contains(label, []byte(placeholderSentinelOpen))
	if needsRewrite {
		link := ast.NewLink()
		link.Destination = expanded
		link.AppendChild(link, ast.NewString(label))
		parent.ReplaceChild(parent, n, link)
		if !safeLinkURL(string(expanded)) {
			liftChildren(link)
		}
		return
	}
	if !safeLinkURL(autoLinkURL(n, source)) {
		parent.ReplaceChild(parent, n, ast.NewString(label))
	}
}

func replaceWithLiteral(n ast.Node, raw []byte) {
	parent := n.Parent()
	if parent == nil {
		return
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		parent.RemoveChild(parent, n)
		return
	}
	parent.ReplaceChild(parent, n, ast.NewString(append([]byte(nil), raw...)))
}

func htmlBlockRaw(n *ast.HTMLBlock, source []byte) []byte {
	raw := n.Lines().Value(source)
	if n.HasClosure() {
		raw = append(raw, n.ClosureLine.Value(source)...)
	}
	return raw
}

func replaceHTMLBlock(n *ast.HTMLBlock, source []byte) {
	parent := n.Parent()
	if parent == nil {
		return
	}
	raw := htmlBlockRaw(n, source)
	if len(bytes.TrimSpace(raw)) == 0 {
		parent.RemoveChild(parent, n)
		return
	}
	p := ast.NewParagraph()
	p.AppendChild(p, ast.NewString(append([]byte(nil), raw...)))
	parent.ReplaceChild(parent, n, p)
}

func liftChildren(n ast.Node) {
	parent := n.Parent()
	if parent == nil {
		return
	}
	for c := n.FirstChild(); c != nil; {
		next := c.NextSibling()
		parent.InsertBefore(parent, n, c)
		c = next
	}
	parent.RemoveChild(parent, n)
}

func autoLinkURL(n *ast.AutoLink, source []byte) string {
	u := string(n.URL(source))
	if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(u), "mailto:") {
		return "mailto:" + u
	}
	return u
}

func safeLinkURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	case "mailto":
		return true
	default:
		return false
	}
}

func renderPlain(doc ast.Node, source []byte) string {
	var w plainWriter
	w.source = source
	w.writeBlocks(doc)
	return w.b.String()
}

type plainWriter struct {
	b      strings.Builder
	source []byte
}

func (w *plainWriter) writeBlocks(n ast.Node) {
	first := true
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if _, ok := c.(*ast.LinkReferenceDefinition); ok {
			continue
		}
		var child plainWriter
		child.source = w.source
		child.writeNode(c)
		text := child.b.String()
		if strings.TrimSpace(text) == "" {
			continue
		}
		if !first {
			w.b.WriteString("\n\n")
		}
		first = false
		w.b.WriteString(strings.Trim(text, "\n"))
	}
}

func (w *plainWriter) writeNode(n ast.Node) {
	switch n := n.(type) {
	case *ast.Document, *ast.Blockquote:
		w.writeBlocks(n)
	case *ast.Paragraph, *ast.Heading, *ast.TextBlock, *ast.Emphasis, *ast.CodeSpan:
		w.writeInlines(n)
	case *ast.Text:
		w.b.Write(n.Value(w.source))
		if n.HardLineBreak() || n.SoftLineBreak() {
			w.b.WriteByte('\n')
		}
	case *ast.String:
		w.b.Write(n.Value)
	case *ast.Link:
		var label plainWriter
		label.source = w.source
		label.writeInlines(n)
		appendLink(&w.b, label.b.String(), string(n.Destination))
	case *ast.AutoLink:
		appendLink(&w.b, string(n.Label(w.source)), autoLinkURL(n, w.source))
	case *ast.Image:
		w.writeInlines(n)
	case *ast.List:
		w.writeList(n, "")
	case *ast.ListItem:
		w.writeListItem(n, "")
	case *ast.CodeBlock, *ast.FencedCodeBlock:
		w.writeLines(n)
	case *ast.ThematicBreak:
		w.b.WriteString("---")
	case *ast.RawHTML:
		if n.Segments != nil {
			w.b.Write(n.Segments.Value(w.source))
		}
	case *ast.HTMLBlock:
		w.b.Write(htmlBlockRaw(n, w.source))
	case *ast.LinkReferenceDefinition:
		return
	default:
		w.writeInlines(n)
	}
}

func (w *plainWriter) writeInlines(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		w.writeNode(c)
	}
}

func (w *plainWriter) writeLines(n ast.Node) {
	if lines := n.Lines(); lines != nil {
		w.b.Write(lines.Value(w.source))
	}
}

func (w *plainWriter) writeList(n *ast.List, indent string) {
	start := n.Start
	if start <= 0 {
		start = 1
	}
	index := 0
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		item, ok := c.(*ast.ListItem)
		if !ok {
			continue
		}
		if index > 0 {
			w.b.WriteByte('\n')
		}
		w.b.WriteString(indent)
		if n.IsOrdered() {
			fmt.Fprintf(&w.b, "%d. ", start)
			start++
		} else {
			w.b.WriteString("- ")
		}
		var body plainWriter
		body.source = w.source
		body.writeListItem(item, indent)
		w.b.WriteString(strings.Trim(body.b.String(), "\n"))
		index++
	}
}

func (w *plainWriter) writeListItem(n *ast.ListItem, indent string) {
	first := true
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if list, ok := c.(*ast.List); ok {
			if !first {
				w.b.WriteByte('\n')
			}
			first = false
			w.writeList(list, indent+"  ")
			continue
		}
		if !first {
			w.b.WriteByte('\n')
		}
		first = false
		w.writeNode(c)
	}
}

func appendLink(b *strings.Builder, label, rawURL string) {
	b.WriteString(label)
	trimmedLabel := strings.TrimSpace(label)
	trimmedURL := strings.TrimSpace(rawURL)
	if trimmedURL == "" || trimmedURL == trimmedLabel || trimmedURL == "mailto:"+trimmedLabel {
		return
	}
	b.WriteString(" (")
	b.WriteString(trimmedURL)
	b.WriteByte(')')
}
