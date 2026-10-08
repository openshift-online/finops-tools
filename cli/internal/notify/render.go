// Package notify renders owner notification emails for account review reports
// and delivers them through Gmail. Rendering is separate from send: the CLI
// plans messages first, then optionally sends with --send --yes or --redirect-prefix.
package notify

import (
	"fmt"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	"github.com/openshift-online/finops-tools/core/accountreview"
)

const responseDeadlineWeeks = 2

const actionRequiredSubjectPrefix = "⚠️ Action required: "

const actionRequiredIntroSuffix = " Based on the information below, please reply to this email and tell us whether you want to:"

const actionRequiredOptions = `- Keep the account
- Delete the account
- Request modifications (describe what you need)`

const generatedBy = "Red Hat Hybrid Platform FinOps"

// incompleteInventoryNote is shown in owner emails when InventoryError is set.
// Raw scan errors stay off the body so owners are not asked to interpret AWS API failures.
const incompleteInventoryNote = "Inventory collection was incomplete. Missing resources are not confirmed unused."

// incompleteOpenShiftNote is shown in owner emails when OpenShiftClustersError is set.
const incompleteOpenShiftNote = "OpenShift cluster lookup was incomplete. Clusters in this account may not be listed."

const (
	htmlTableStyle  = `width:100%;border-collapse:collapse;font-size:.9rem`
	htmlCellStyle   = `text-align:left;padding:.4rem .6rem;border-bottom:1px solid #d0d7de`
	htmlAmountStyle = `text-align:right;white-space:nowrap;padding:.4rem .6rem;border-bottom:1px solid #d0d7de`
	htmlMetaStyle   = `color:#656d76;font-size:.9rem`
	htmlActionStyle = `background:#fff8c5;border:1px solid #d4a72c;border-radius:8px;padding:1rem 1.25rem;margin-bottom:1rem`
	htmlCardStyle   = `background:#fff;border:1px solid #d0d7de;border-radius:8px;padding:1rem 1.25rem;margin-bottom:1rem`
	htmlH4Style     = `margin:.75rem 0 .35rem;font-size:.9rem;font-weight:600;color:#656d76`
)

func actionRequiredIntro(details []accountreview.AccountDetails) string {
	switch len(details) {
	case 1:
		d := details[0]
		return fmt.Sprintf(
			"We are reviewing AWS account %s (%s), which may no longer be needed.%s",
			d.AccountName, d.AccountID, actionRequiredIntroSuffix,
		)
	default:
		if len(details) > 1 {
			return fmt.Sprintf(
				"We are reviewing the %d AWS accounts below, which may no longer be needed.%s",
				len(details), actionRequiredIntroSuffix,
			)
		}
		return "We are reviewing AWS accounts that may no longer be needed." + actionRequiredIntroSuffix
	}
}

func actionRequiredDeadlineText(accountCount int, generated time.Time) string {
	phrase := "this AWS account"
	if accountCount > 1 {
		phrase = "these AWS accounts"
	}
	return fmt.Sprintf(
		"If we do not hear from you by %s, we will proceed with deleting %s.",
		replyByDate(generated).Format("2 January 2006"),
		phrase,
	)
}

func replyByDate(generated time.Time) time.Time {
	return generated.UTC().AddDate(0, 0, responseDeadlineWeeks*7)
}

func groupReviewSubject(n int) string {
	if n == 1 {
		return actionRequiredSubjectPrefix + "AWS account review: 1 AWS account"
	}
	return actionRequiredSubjectPrefix + fmt.Sprintf("AWS account review: %d AWS accounts", n)
}

type Message struct {
	// To is the envelope recipient after redirect/owner resolution (same as DeliveryTo).
	To string `json:"to"`
	// IntendedTo is the resolved owner mailbox, even when DeliveryTo is a redirect address.
	IntendedTo string `json:"intended_to,omitempty"`
	// DeliveryTo is where Gmail actually sends (owner or PREFIX+owner@redhat.com).
	DeliveryTo string `json:"delivery_to,omitempty"`
	Subject    string `json:"subject"`
	TextBody   string `json:"text_body"`
	HTMLBody   string `json:"html_body"`
}

// RenderAccountEmail renders a single-account notification.
func RenderAccountEmail(d accountreview.AccountDetails) Message {
	subject := actionRequiredSubjectPrefix + fmt.Sprintf("AWS account review: %s (%s)", d.AccountName, d.AccountID)
	return Message{
		To:         d.OwnerEmail,
		IntendedTo: d.OwnerEmail,
		Subject:    subject,
		TextBody:   renderTextBody([]accountreview.AccountDetails{d}),
		HTMLBody:   renderHTMLBody([]accountreview.AccountDetails{d}),
	}
}

// RenderOwnerGroupEmail renders a multi-account notification for one owner.
func RenderOwnerGroupEmail(ownerEmail string, details []accountreview.AccountDetails) Message {
	return Message{
		To:         ownerEmail,
		IntendedTo: ownerEmail,
		Subject:    groupReviewSubject(len(details)),
		TextBody:   renderTextBody(details),
		HTMLBody:   renderHTMLBody(details),
	}
}

func renderTextBody(details []accountreview.AccountDetails) string {
	var b strings.Builder
	generated := generatedAt(details)
	writeActionRequiredText(&b, details, generated)
	if len(details) > 1 {
		b.WriteString("\n")
		writeOverviewText(&b, details)
		b.WriteString(strings.Repeat("=", 48))
		b.WriteString("\n")
	}
	for i, d := range details {
		if i > 0 {
			b.WriteString("\n\n")
			b.WriteString(strings.Repeat("=", 48))
			b.WriteString("\n\n")
		} else {
			b.WriteString("\n")
		}
		writeAccountText(&b, d)
	}
	fmt.Fprintf(&b, "\n\nGenerated %s\n%s\n", generated.Format("2006-01-02 15:04 UTC"), generatedBy)
	return b.String()
}

func writeOverviewText(b *strings.Builder, details []accountreview.AccountDetails) {
	b.WriteString("Accounts in this review\n")
	for _, d := range details {
		last, total := "-", "-"
		if l, t, ok := costSummaryAmounts(d); ok {
			last, total = l, t
		}
		fmt.Fprintf(b, "  %s (%s)  last listed month: %s  period total: %s\n", d.AccountName, d.AccountID, last, total)
	}
	b.WriteByte('\n')
}

func writeAccountText(b *strings.Builder, d accountreview.AccountDetails) {
	fmt.Fprintf(b, "Account: %s (%s)\n", d.AccountName, d.AccountID)
	if line := costSummaryLine(d); line != "" {
		fmt.Fprintf(b, "%s\n", line)
	}
	if d.DisplayAlias != "" {
		fmt.Fprintf(b, "Alias: %s\n", d.DisplayAlias)
	}
	if d.OUPath != "" {
		fmt.Fprintf(b, "OU: %s\n", d.OUPath)
	}
	if d.OwnerError != "" {
		fmt.Fprintf(b, "Owner: %s\n", d.OwnerError)
	}
	if len(d.Tags) > 0 {
		b.WriteString("Tags:\n")
		for _, t := range d.Tags {
			fmt.Fprintf(b, "  %s: %s\n", t.Key, t.Value)
		}
	}

	b.WriteString("\nMonthly costs (net amortized):\n")
	mc := d.MonthlyCosts
	if mc.Error != "" {
		fmt.Fprintf(b, "  (unavailable: %s)\n", mc.Error)
	} else if len(mc.Months) == 0 {
		b.WriteString("  (no data)\n")
	} else {
		for _, m := range mc.Months {
			fmt.Fprintf(b, "  %s: %s\n", m.Month, format.FormatMoney(m.Amount, mc.Currency))
		}
		fmt.Fprintf(b, "  Total: %s\n", format.FormatMoney(mc.Total, mc.Currency))
	}

	if mc.Error == "" && len(mc.TopServices) > 0 {
		b.WriteString("\nTop services (last complete month):\n")
		for _, svc := range mc.TopServices {
			fmt.Fprintf(b, "  %s: %s\n", svc.Service, format.FormatMoney(svc.Amount, mc.Currency))
		}
	} else if mc.Error == "" && mc.TopServicesError != "" {
		fmt.Fprintf(b, "\nTop services (last complete month):\n  (unavailable: %s)\n", mc.TopServicesError)
	}

	writeInventoryText(b, d)
}

func writeInventoryText(b *strings.Builder, d accountreview.AccountDetails) {
	b.WriteString("\nResources:\n")
	tables := d.NonemptyInventoryTables()
	counts := d.NonzeroInventoryCounts()
	for _, table := range tables {
		fmt.Fprintf(b, "  %s: %d\n", table.Title, table.Count)
		for _, row := range table.Rows {
			b.WriteString("    ")
			b.WriteString(strings.Join(row, "  "))
			b.WriteByte('\n')
		}
	}
	if len(counts) > 0 || d.NoneFoundLine() != "" {
		if len(tables) > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("  Other resources:\n")
		for _, c := range counts {
			fmt.Fprintf(b, "    %s: %d\n", c.Title, c.Count)
		}
		if line := d.NoneFoundLine(); line != "" {
			fmt.Fprintf(b, "    %s\n", line)
		}
	}
	writeOpenShiftText(b, d)
	writeWarningsText(b, d)
}

func writeWarningsText(b *strings.Builder, d accountreview.AccountDetails) {
	invNote := incompleteInventoryLine(d)
	ocpNote := incompleteOpenShiftLine(d)
	if invNote == "" && ocpNote == "" {
		return
	}
	b.WriteString("\nWarnings:\n")
	if invNote != "" {
		fmt.Fprintf(b, "  %s\n", invNote)
	}
	if ocpNote != "" {
		fmt.Fprintf(b, "  %s\n", ocpNote)
	}
}

func writeOpenShiftText(b *strings.Builder, d accountreview.AccountDetails) {
	if !d.HasOpenShiftSection() {
		return
	}
	table := d.OpenShiftTable()
	fmt.Fprintf(b, "\n%s:\n", d.OpenShiftSectionTitle())
	if table.Count > 0 {
		for _, row := range table.Rows {
			b.WriteString("  ")
			b.WriteString(strings.Join(row, "  "))
			b.WriteByte('\n')
		}
	} else if line := d.OpenShiftNoneFoundLine(); line != "" {
		fmt.Fprintf(b, "  %s\n", line)
	}
	if note := incompleteOpenShiftLine(d); note != "" {
		fmt.Fprintf(b, "  %s\n", note)
	}
}

func writeActionRequiredText(b *strings.Builder, details []accountreview.AccountDetails, generated time.Time) {
	b.WriteString("Action required\n\n")
	b.WriteString(actionRequiredIntro(details))
	b.WriteString("\n\n")
	b.WriteString(actionRequiredOptions)
	b.WriteString("\n\n")
	b.WriteString(actionRequiredDeadlineText(len(details), generated))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("=", 48))
	b.WriteString("\n")
}

func renderHTMLBody(details []accountreview.AccountDetails) string {
	var b strings.Builder
	generated := generatedAt(details)
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><style>
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;line-height:1.5;color:#1f2328;background:#f6f8fa;margin:0;padding:1.5rem}
.container{max-width:900px;margin:0 auto}
section{background:#fff;border:1px solid #d0d7de;border-radius:8px;padding:1rem 1.25rem;margin-bottom:1rem}
h1{margin:0 0 .5rem;font-size:1.5rem}h2{margin:0 0 .75rem;font-size:1.1rem;color:#0969da}
h3{margin:1rem 0 .5rem;font-size:1rem;color:#1f2328}
h4{margin:.75rem 0 .35rem;font-size:.9rem;font-weight:600;color:#656d76}
table{width:100%;border-collapse:collapse;font-size:.9rem}th,td{text-align:left;padding:.4rem .6rem;border-bottom:1px solid #d0d7de}
th{color:#656d76}.amount{text-align:right;white-space:nowrap}.meta{color:#656d76;font-size:.9rem}
.action{background:#fff8c5;border:1px solid #d4a72c;border-radius:8px;padding:1rem 1.25rem;margin-bottom:1rem}
.action h2{margin:0 0 .75rem;font-size:1.1rem;color:#9a6700}
.action ul{margin:.5rem 0 0 1.25rem;padding:0}
footer{margin-top:1rem;color:#656d76;font-size:.85rem;text-align:center}
</style></head><body><div class="container">`)
	fmt.Fprintf(&b, `<header><h1>AWS account review</h1><p class="meta" style="%s">Generated %s</p></header>`, htmlMetaStyle, generated.Format("2006-01-02 15:04 UTC"))
	writeActionRequiredHTML(&b, details, generated)
	if len(details) > 1 {
		writeOverviewHTML(&b, details)
	}
	for _, d := range details {
		writeAccountHTML(&b, d)
	}
	fmt.Fprintf(&b, `<footer style="%s;text-align:center">%s</footer></div></body></html>`, htmlMetaStyle, htmlEscape(generatedBy))
	return b.String()
}

func writeOverviewHTML(b *strings.Builder, details []accountreview.AccountDetails) {
	fmt.Fprintf(b, `<section style="%s"><h3>Accounts in this review</h3>`, htmlCardStyle)
	writeHTMLTableOpen(b)
	b.WriteString(`<thead><tr>`)
	writeHTMLHeaderCell(b, "Account", false)
	writeHTMLHeaderCell(b, "ID", false)
	writeHTMLHeaderCell(b, "Last listed month", true)
	writeHTMLHeaderCell(b, "Period total", true)
	b.WriteString(`</tr></thead><tbody>`)
	for _, d := range details {
		last, total := "-", "-"
		if l, t, ok := costSummaryAmounts(d); ok {
			last, total = l, t
		}
		b.WriteString(`<tr>`)
		writeHTMLCell(b, d.AccountName, false)
		writeHTMLCell(b, d.AccountID, false)
		writeHTMLCell(b, last, true)
		writeHTMLCell(b, total, true)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table></section>`)
}

func writeAccountHTML(b *strings.Builder, d accountreview.AccountDetails) {
	fmt.Fprintf(b, `<section style="%s"><h2>%s (%s)</h2>`, htmlCardStyle, htmlEscape(d.AccountName), htmlEscape(d.AccountID))
	if line := costSummaryLine(d); line != "" {
		fmt.Fprintf(b, `<p class="meta" style="%s">%s</p>`, htmlMetaStyle, htmlEscape(line))
	}
	if d.DisplayAlias != "" {
		fmt.Fprintf(b, `<p class="meta" style="%s">Alias: %s</p>`, htmlMetaStyle, htmlEscape(d.DisplayAlias))
	}
	if d.OUPath != "" {
		fmt.Fprintf(b, `<p class="meta" style="%s">OU: %s</p>`, htmlMetaStyle, htmlEscape(d.OUPath))
	}
	if d.OwnerError != "" {
		fmt.Fprintf(b, `<p class="meta" style="%s">Owner: %s</p>`, htmlMetaStyle, htmlEscape(d.OwnerError))
	}

	if len(d.Tags) > 0 {
		b.WriteString(`<h3>Tags</h3>`)
		writeHTMLTableOpen(b)
		b.WriteString(`<thead><tr>`)
		writeHTMLHeaderCell(b, "Tag", false)
		writeHTMLHeaderCell(b, "Value", false)
		b.WriteString(`</tr></thead><tbody>`)
		for _, t := range d.Tags {
			b.WriteString(`<tr>`)
			writeHTMLCell(b, t.Key, false)
			writeHTMLCell(b, t.Value, false)
			b.WriteString(`</tr>`)
		}
		b.WriteString(`</tbody></table>`)
	}

	b.WriteString(`<h3>Monthly costs (net amortized)</h3>`)
	mc := d.MonthlyCosts
	if mc.Error != "" {
		fmt.Fprintf(b, `<p class="meta" style="%s">Unavailable: %s</p>`, htmlMetaStyle, htmlEscape(mc.Error))
	} else if len(mc.Months) == 0 {
		fmt.Fprintf(b, `<p class="meta" style="%s">(no data)</p>`, htmlMetaStyle)
	} else {
		writeHTMLTableOpen(b)
		b.WriteString(`<thead><tr>`)
		writeHTMLHeaderCell(b, "Month", false)
		writeHTMLHeaderCell(b, "Amount", true)
		b.WriteString(`</tr></thead><tbody>`)
		for _, m := range mc.Months {
			b.WriteString(`<tr>`)
			writeHTMLCell(b, m.Month, false)
			writeHTMLCell(b, format.FormatMoney(m.Amount, mc.Currency), true)
			b.WriteString(`</tr>`)
		}
		if len(mc.Months) > 0 {
			b.WriteString(`<tr>`)
			writeHTMLHeaderCell(b, "Total", false)
			writeHTMLCell(b, format.FormatMoney(mc.Total, mc.Currency), true)
			b.WriteString(`</tr>`)
		}
		b.WriteString(`</tbody></table>`)
	}

	if mc.Error == "" && len(mc.TopServices) > 0 {
		b.WriteString(`<h3>Top services (last complete month)</h3>`)
		writeHTMLTableOpen(b)
		b.WriteString(`<thead><tr>`)
		writeHTMLHeaderCell(b, "Service", false)
		writeHTMLHeaderCell(b, "Amount", true)
		b.WriteString(`</tr></thead><tbody>`)
		for _, svc := range mc.TopServices {
			b.WriteString(`<tr>`)
			writeHTMLCell(b, svc.Service, false)
			writeHTMLCell(b, format.FormatMoney(svc.Amount, mc.Currency), true)
			b.WriteString(`</tr>`)
		}
		b.WriteString(`</tbody></table>`)
	} else if mc.Error == "" && mc.TopServicesError != "" {
		fmt.Fprintf(b, `<h3>Top services (last complete month)</h3><p class="meta" style="%s">Unavailable: %s</p>`, htmlMetaStyle, htmlEscape(mc.TopServicesError))
	}

	writeInventoryHTML(b, d)
	b.WriteString(`</section>`)
}

func writeInventoryHTML(b *strings.Builder, d accountreview.AccountDetails) {
	b.WriteString(`<h3>Resources</h3>`)
	tables := d.NonemptyInventoryTables()
	counts := d.NonzeroInventoryCounts()
	for _, table := range tables {
		writeResourceTable(b, table)
	}
	if len(counts) > 0 || d.NoneFoundLine() != "" {
		fmt.Fprintf(b, `<h4 style="%s">Other resources</h4>`, htmlH4Style)
		if len(counts) > 0 {
			fmt.Fprintf(b, `<p class="meta" style="%s">`, htmlMetaStyle)
			for i, c := range counts {
				if i > 0 {
					b.WriteString(" · ")
				}
				fmt.Fprintf(b, `%s: %d`, htmlEscape(c.Title), c.Count)
			}
			b.WriteString(`</p>`)
		}
		if line := d.NoneFoundLine(); line != "" {
			fmt.Fprintf(b, `<p class="meta" style="%s">%s</p>`, htmlMetaStyle, htmlEscape(line))
		}
	}
	writeOpenShiftHTML(b, d)
	writeWarningsHTML(b, d)
}

func writeWarningsHTML(b *strings.Builder, d accountreview.AccountDetails) {
	invNote := incompleteInventoryLine(d)
	ocpNote := incompleteOpenShiftLine(d)
	if invNote == "" && ocpNote == "" {
		return
	}
	b.WriteString(`<h3>Warnings</h3>`)
	if invNote != "" {
		fmt.Fprintf(b, `<p class="meta" style="%s">%s</p>`, htmlMetaStyle, htmlEscape(invNote))
	}
	if ocpNote != "" {
		fmt.Fprintf(b, `<p class="meta" style="%s">%s</p>`, htmlMetaStyle, htmlEscape(ocpNote))
	}
}

func writeOpenShiftHTML(b *strings.Builder, d accountreview.AccountDetails) {
	if !d.HasOpenShiftSection() {
		return
	}
	table := d.OpenShiftTable()
	fmt.Fprintf(b, `<h3>%s</h3>`, htmlEscape(d.OpenShiftSectionTitle()))
	if table.Count > 0 {
		writeHTMLTableOpen(b)
		b.WriteString(`<thead><tr>`)
		for _, h := range table.Headers {
			writeHTMLHeaderCell(b, h, false)
		}
		b.WriteString(`</tr></thead><tbody>`)
		for _, row := range table.Rows {
			b.WriteString(`<tr>`)
			for _, cell := range row {
				writeHTMLCell(b, cell, false)
			}
			b.WriteString(`</tr>`)
		}
		b.WriteString(`</tbody></table>`)
	} else if line := d.OpenShiftNoneFoundLine(); line != "" {
		fmt.Fprintf(b, `<p class="meta" style="%s">%s</p>`, htmlMetaStyle, htmlEscape(line))
	}
}

func generatedAt(details []accountreview.AccountDetails) time.Time {
	if len(details) > 0 && !details[0].GeneratedAt.IsZero() {
		return details[0].GeneratedAt.UTC()
	}
	return time.Now().UTC()
}

func writeResourceTable(b *strings.Builder, table accountreview.ResourceTable) {
	fmt.Fprintf(b, `<h4 style="%s">%s (%d)</h4>`, htmlH4Style, htmlEscape(table.Title), table.Count)
	if table.Count == 0 {
		return
	}
	writeHTMLTableOpen(b)
	b.WriteString(`<thead><tr>`)
	for _, h := range table.Headers {
		writeHTMLHeaderCell(b, h, false)
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, row := range table.Rows {
		b.WriteString(`<tr>`)
		for _, cell := range row {
			writeHTMLCell(b, cell, false)
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table>`)
}

func writeActionRequiredHTML(b *strings.Builder, details []accountreview.AccountDetails, generated time.Time) {
	fmt.Fprintf(b, `<section class="action" style="%s"><h2>Action required</h2><p>`, htmlActionStyle)
	b.WriteString(htmlEscape(actionRequiredIntro(details)))
	b.WriteString(`</p><ul><li>Keep the account</li><li>Delete the account</li><li>Request modifications (describe what you need)</li></ul><p><strong>`)
	b.WriteString(htmlEscape(actionRequiredDeadlineText(len(details), generated)))
	b.WriteString(`</strong></p></section>`)
}

func writeHTMLTableOpen(b *strings.Builder) {
	fmt.Fprintf(b, `<table style="%s">`, htmlTableStyle)
}

func writeHTMLHeaderCell(b *strings.Builder, text string, amount bool) {
	style := htmlCellStyle + `;color:#656d76`
	if amount {
		style = htmlAmountStyle + `;color:#656d76`
	}
	fmt.Fprintf(b, `<th style="%s">%s</th>`, style, htmlEscape(text))
}

func writeHTMLCell(b *strings.Builder, text string, amount bool) {
	style := htmlCellStyle
	class := ""
	if amount {
		style = htmlAmountStyle
		class = ` class="amount"`
	}
	fmt.Fprintf(b, `<td%s style="%s">%s</td>`, class, style, htmlEscape(text))
}

func incompleteInventoryLine(d accountreview.AccountDetails) string {
	if strings.TrimSpace(d.InventoryError) == "" {
		return ""
	}
	return incompleteInventoryNote
}

func incompleteOpenShiftLine(d accountreview.AccountDetails) string {
	if strings.TrimSpace(d.OpenShiftClustersError) == "" {
		return ""
	}
	return incompleteOpenShiftNote
}

func costSummaryLine(d accountreview.AccountDetails) string {
	last, total, ok := costSummaryAmounts(d)
	if !ok {
		return ""
	}
	return fmt.Sprintf("Last listed month: %s · Period total: %s", last, total)
}

func costSummaryAmounts(d accountreview.AccountDetails) (last, total string, ok bool) {
	mc := d.MonthlyCosts
	if mc.Error != "" || len(mc.Months) == 0 {
		return "", "", false
	}
	m := mc.Months[len(mc.Months)-1]
	return format.FormatMoney(m.Amount, mc.Currency), format.FormatMoney(mc.Total, mc.Currency), true
}

func htmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return replacer.Replace(s)
}
