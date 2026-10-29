// Package notify renders owner notification emails for account review reports
// and delivers them through Gmail. Rendering is separate from send: the CLI
// plans messages first, then optionally sends with --send --yes or --redirect-prefix.
package notify

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	"github.com/openshift-online/finops-tools/core/accountreview"
)

// defaultReplyByDays is the built-in response window used for the {date}
// placeholder when a deadline sentence is requested without --email-reply-by-days.
const defaultReplyByDays = 14

const actionRequiredSubjectPrefix = "⚠️ Action required: "

const actionRequiredTitle = "Action required"

const actionRequiredIntroSuffix = " Based on the information below, please reply to this email and tell us whether you want to:"

// actionRequiredOptionList is the built-in reply options list.
var actionRequiredOptionList = []string{
	"Keep the account",
	"Delete the account",
	"Request modifications (describe what you need)",
}

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

// RenderOptions customizes rendered owner notification emails. The zero value
// renders the built-in template with no deadline sentence: deletion is not the
// default outcome, so a reply deadline is only shown when DeadlineText is set.
type RenderOptions struct {
	// SubjectPrefix overrides the built-in subject prefix (used verbatim,
	// including any trailing space).
	SubjectPrefix string
	// ActionTitle overrides the "Action required" section heading.
	ActionTitle string
	// Intro overrides the whole intro paragraph, including the reply
	// instruction. Placeholders: {account}, {account_id}, {count}.
	Intro string
	// Options overrides the reply options list.
	Options []string
	// DeadlineText adds a deadline sentence with a {date} placeholder.
	// Empty omits the deadline sentence (and the reply-by date) entirely.
	DeadlineText string
	// ReplyByDays sets the {date} offset in days from report generation.
	// Zero falls back to the built-in default of 14 days.
	ReplyByDays int
}

func (o RenderOptions) subjectPrefix() string {
	if o.SubjectPrefix != "" {
		return o.SubjectPrefix
	}
	return actionRequiredSubjectPrefix
}

func (o RenderOptions) title() string {
	if o.ActionTitle != "" {
		return o.ActionTitle
	}
	return actionRequiredTitle
}

func (o RenderOptions) optionList() []string {
	if len(o.Options) > 0 {
		return o.Options
	}
	return actionRequiredOptionList
}

// ValidateIntroTemplate returns an error when intro contains a placeholder
// other than {account}, {account_id}, or {count}. An empty intro is valid.
func ValidateIntroTemplate(intro string) error {
	return validatePlaceholders("email intro", intro, "account", "account_id", "count")
}

// ValidateDeadlineTemplate returns an error when text contains a placeholder
// other than {date}. An empty text is valid.
func ValidateDeadlineTemplate(text string) error {
	return validatePlaceholders("email deadline", text, "date")
}

// placeholderPattern matches {token} template placeholders.
var placeholderPattern = regexp.MustCompile(`\{([^{}]*)\}`)

// validatePlaceholders rejects {token} occurrences outside the allowed set so
// placeholder typos fail before any AWS work or email send.
func validatePlaceholders(where, s string, allowed ...string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowedSet[a] = true
	}
	for _, m := range placeholderPattern.FindAllStringSubmatch(s, -1) {
		if !allowedSet[m[1]] {
			return fmt.Errorf("unknown placeholder {%s} in %s (supported: %s)", m[1], where, strings.Join(allowed, ", "))
		}
	}
	return nil
}

// expandPlaceholders replaces {token} placeholders with values. Unknown tokens
// are left untouched; the CLI validates templates before rendering.
func expandPlaceholders(s string, values map[string]string) string {
	repl := make([]string, 0, 2*len(values))
	for k, v := range values {
		repl = append(repl, "{"+k+"}", v)
	}
	return strings.NewReplacer(repl...).Replace(s)
}

func actionRequiredIntro(details []accountreview.AccountDetails, opts RenderOptions) string {
	if opts.Intro != "" {
		return expandPlaceholders(opts.Intro, introPlaceholderValues(details))
	}
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

// introPlaceholderValues maps the intro placeholders: {account} is the account
// reference ("AWS account name (id)" for a single-account email, "the N AWS
// accounts below" otherwise), {account_id} is the account ID (empty for
// grouped emails), and {count} is the number of accounts in the email.
func introPlaceholderValues(details []accountreview.AccountDetails) map[string]string {
	if len(details) == 1 {
		d := details[0]
		return map[string]string{
			"account":    fmt.Sprintf("AWS account %s (%s)", d.AccountName, d.AccountID),
			"account_id": d.AccountID,
			"count":      "1",
		}
	}
	return map[string]string{
		"account":    fmt.Sprintf("the %d AWS accounts below", len(details)),
		"account_id": "",
		"count":      strconv.Itoa(len(details)),
	}
}

// actionRequiredDeadlineText renders the configured deadline sentence. ok is
// false when no deadline is configured; callers omit the sentence entirely.
func actionRequiredDeadlineText(generated time.Time, opts RenderOptions) (string, bool) {
	if opts.DeadlineText == "" {
		return "", false
	}
	return expandPlaceholders(opts.DeadlineText, map[string]string{
		"date": replyByDate(generated, opts).Format("2 January 2006"),
	}), true
}

func replyByDate(generated time.Time, opts RenderOptions) time.Time {
	days := opts.ReplyByDays
	if days <= 0 {
		days = defaultReplyByDays
	}
	return generated.UTC().AddDate(0, 0, days)
}

func groupReviewSubject(n int, opts RenderOptions) string {
	if n == 1 {
		return opts.subjectPrefix() + "AWS account review: 1 AWS account"
	}
	return opts.subjectPrefix() + fmt.Sprintf("AWS account review: %d AWS accounts", n)
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
func RenderAccountEmail(d accountreview.AccountDetails, opts RenderOptions) Message {
	subject := opts.subjectPrefix() + fmt.Sprintf("AWS account review: %s (%s)", d.AccountName, d.AccountID)
	return Message{
		To:         d.OwnerEmail,
		IntendedTo: d.OwnerEmail,
		Subject:    subject,
		TextBody:   renderTextBody([]accountreview.AccountDetails{d}, opts),
		HTMLBody:   renderHTMLBody([]accountreview.AccountDetails{d}, opts),
	}
}

// RenderOwnerGroupEmail renders a multi-account notification for one owner.
func RenderOwnerGroupEmail(ownerEmail string, details []accountreview.AccountDetails, opts RenderOptions) Message {
	return Message{
		To:         ownerEmail,
		IntendedTo: ownerEmail,
		Subject:    groupReviewSubject(len(details), opts),
		TextBody:   renderTextBody(details, opts),
		HTMLBody:   renderHTMLBody(details, opts),
	}
}

func renderTextBody(details []accountreview.AccountDetails, opts RenderOptions) string {
	var b strings.Builder
	generated := generatedAt(details)
	writeActionRequiredText(&b, details, generated, opts)
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
	if note := d.InventoryScopeNote(); note != "" {
		fmt.Fprintf(b, "  %s\n", note)
	}
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
}

func writeActionRequiredText(b *strings.Builder, details []accountreview.AccountDetails, generated time.Time, opts RenderOptions) {
	fmt.Fprintf(b, "%s\n\n", opts.title())
	b.WriteString(actionRequiredIntro(details, opts))
	b.WriteString("\n\n")
	b.WriteString(optionListText(opts))
	if text, ok := actionRequiredDeadlineText(generated, opts); ok {
		b.WriteString("\n\n")
		b.WriteString(text)
	}
	b.WriteString("\n")
	b.WriteString(strings.Repeat("=", 48))
	b.WriteString("\n")
}

func optionListText(opts RenderOptions) string {
	options := opts.optionList()
	lines := make([]string, len(options))
	for i, o := range options {
		lines[i] = "- " + o
	}
	return strings.Join(lines, "\n")
}

func renderHTMLBody(details []accountreview.AccountDetails, opts RenderOptions) string {
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
	writeActionRequiredHTML(&b, details, generated, opts)
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
	if note := d.InventoryScopeNote(); note != "" {
		fmt.Fprintf(b, `<p class="meta" style="%s">%s</p>`, htmlMetaStyle, htmlEscape(note))
	}
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

func writeActionRequiredHTML(b *strings.Builder, details []accountreview.AccountDetails, generated time.Time, opts RenderOptions) {
	fmt.Fprintf(b, `<section class="action" style="%s"><h2>%s</h2><p>`, htmlActionStyle, htmlEscape(opts.title()))
	b.WriteString(htmlEscape(actionRequiredIntro(details, opts)))
	b.WriteString(`</p><ul>`)
	for _, opt := range opts.optionList() {
		fmt.Fprintf(b, `<li>%s</li>`, htmlEscape(opt))
	}
	if text, ok := actionRequiredDeadlineText(generated, opts); ok {
		b.WriteString(`</ul><p><strong>`)
		b.WriteString(htmlEscape(text))
		b.WriteString(`</strong></p></section>`)
		return
	}
	b.WriteString(`</ul></section>`)
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
