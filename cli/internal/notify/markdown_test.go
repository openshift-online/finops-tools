package notify

import (
	"strings"
	"testing"
)

func TestRenderTemplateMarkdownEmphasisAndLink(t *testing.T) {
	got := renderTemplateMarkdown("See **bold** and *italic* in the [runbook](https://example.com/docs).", nil, false)
	if !strings.Contains(got.HTML, "<strong>bold</strong>") || !strings.Contains(got.HTML, "<em>italic</em>") {
		t.Fatalf("html = %s", got.HTML)
	}
	if !strings.Contains(got.HTML, `<a style="color:#0969da" href="https://example.com/docs">runbook</a>`) {
		t.Fatalf("link html = %s", got.HTML)
	}
	if got.Plain != "See bold and italic in the runbook (https://example.com/docs)." {
		t.Fatalf("plain = %q", got.Plain)
	}

	inline := renderTemplateMarkdown("*Please respond*", nil, true)
	if inline.HTML != "<em>Please respond</em>" {
		t.Fatalf("inline html = %q", inline.HTML)
	}
	if inline.Plain != "Please respond" {
		t.Fatalf("inline plain = %q", inline.Plain)
	}

	mailto := renderTemplateMarkdown("[mail](mailto:finops@example.com)", nil, true)
	if !strings.Contains(mailto.HTML, `href="mailto:finops@example.com"`) {
		t.Fatalf("mailto html = %s", mailto.HTML)
	}

	auto := renderTemplateMarkdown("<https://example.com/docs>", nil, true)
	if !strings.Contains(auto.HTML, `href="https://example.com/docs"`) {
		t.Fatalf("autolink html = %s", auto.HTML)
	}
	if auto.Plain != "https://example.com/docs" {
		t.Fatalf("autolink plain = %q", auto.Plain)
	}

	email := renderTemplateMarkdown("<finops@example.com>", nil, true)
	if !strings.Contains(email.HTML, `href="mailto:finops@example.com"`) {
		t.Fatalf("email autolink html = %s", email.HTML)
	}
	if email.Plain != "finops@example.com" {
		t.Fatalf("email autolink plain = %q", email.Plain)
	}
}

func TestRenderTemplateMarkdownPlaceholderIsLiteral(t *testing.T) {
	got := renderTemplateMarkdown("See **{account}**.", map[string]string{
		"account": "*prod* <x> & co",
	}, false)
	if !strings.Contains(got.HTML, "<strong>*prod* &lt;x&gt; &amp; co</strong>") {
		t.Fatalf("html = %s", got.HTML)
	}
	if strings.Contains(got.HTML, "<em>") || strings.Contains(got.HTML, "<x>") || strings.Contains(got.HTML, "&amp;amp;") {
		t.Fatalf("placeholder value was reparsed or double-escaped: %s", got.HTML)
	}
	if got.Plain != "See *prod* <x> & co." {
		t.Fatalf("plain = %q", got.Plain)
	}

	link := renderTemplateMarkdown("[id](https://example.com/{account_id})", map[string]string{
		"account_id": "111111111111",
	}, true)
	if !strings.Contains(link.HTML, `href="https://example.com/111111111111"`) {
		t.Fatalf("link html = %s", link.HTML)
	}
}

func TestRenderTemplateMarkdownDropsUnsafeLinksAndImages(t *testing.T) {
	link := renderTemplateMarkdown("Please [click](javascript:alert(1)).", nil, false)
	if strings.Contains(link.HTML, "javascript:") || strings.Contains(link.HTML, "<a ") {
		t.Fatalf("unsafe link should not be an anchor: %s", link.HTML)
	}
	if !strings.Contains(link.HTML, "click") || !strings.Contains(link.Plain, "click") {
		t.Fatalf("link text should remain: html=%s plain=%s", link.HTML, link.Plain)
	}

	for _, src := range []string{
		"See [docs](/internal).",
		"See [files](ftp://example.com/a).",
		"See [data](data:text/html,hi).",
	} {
		got := renderTemplateMarkdown(src, nil, false)
		if strings.Contains(got.HTML, "<a ") {
			t.Fatalf("non-http link should not be an anchor: %s", got.HTML)
		}
	}

	image := renderTemplateMarkdown("See ![diagram](https://example.com/a.png).", nil, false)
	if strings.Contains(image.HTML, "<img") || strings.Contains(image.HTML, "example.com/a.png") {
		t.Fatalf("image should become alt text: %s", image.HTML)
	}
	if !strings.Contains(image.HTML, "diagram") || !strings.Contains(image.Plain, "diagram") {
		t.Fatalf("alt text missing: html=%s plain=%s", image.HTML, image.Plain)
	}
	if strings.Contains(image.Plain, "example.com/a.png") {
		t.Fatalf("plain text should not include the image URL: %s", image.Plain)
	}
}

func TestRenderTemplateMarkdownEscapesRawHTML(t *testing.T) {
	title := renderTemplateMarkdown(`<script>title</script>`, nil, true)
	if strings.Contains(title.HTML, "<script>") || !strings.Contains(title.HTML, "&lt;script&gt;title&lt;/script&gt;") {
		t.Fatalf("title html = %s", title.HTML)
	}

	opt := renderTemplateMarkdown(`<b>opt</b>`, nil, true)
	if strings.Contains(opt.HTML, "<b>") || !strings.Contains(opt.HTML, "&lt;b&gt;opt&lt;/b&gt;") {
		t.Fatalf("option html = %s", opt.HTML)
	}
}

func TestRenderTemplateMarkdownHardWrapAndList(t *testing.T) {
	got := renderTemplateMarkdown("Line one\nLine two\n\n- Alpha\n- Beta", nil, false)
	if !strings.Contains(got.HTML, "Line one<br>") || !strings.Contains(got.HTML, "<ul>") || !strings.Contains(got.HTML, "<li>Alpha</li>") || !strings.Contains(got.HTML, "<li>Beta</li>") {
		t.Fatalf("html = %s", got.HTML)
	}
	if !strings.Contains(got.Plain, "Line one\nLine two") || !strings.Contains(got.Plain, "- Alpha\n- Beta") {
		t.Fatalf("plain = %q", got.Plain)
	}

	ordered := renderTemplateMarkdown("1. First\n2. Second", nil, false)
	if !strings.Contains(ordered.HTML, "<ol>") || !strings.Contains(ordered.Plain, "1. First\n2. Second") {
		t.Fatalf("ordered html=%s plain=%q", ordered.HTML, ordered.Plain)
	}
}
