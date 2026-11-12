// account_list_tags_pretty.go renders account tags as a colorized table.
package output

import (
	"fmt"
	"io"

	"github.com/olekukonko/tablewriter"
)

func writeAWSAccountTagsPretty(w io.Writer, view AccountTagsView) error {
	s := newStyler(w)
	target := formatAccountTagTarget(view)
	if len(view.Tags) == 0 {
		msg := fmt.Sprintf("No AWS account tags found for %s.", target)
		if s.enabled {
			msg = s.dim(msg)
		}
		_, err := fmt.Fprintln(w, msg)
		return err
	}

	label := "Account:"
	if s.enabled {
		label = s.dim(label)
		target = s.bold(target)
	}
	if _, err := fmt.Fprintf(w, "  %s  %s\n\n", label, target); err != nil {
		return err
	}

	title := "AWS account tags"
	if s.enabled {
		title = s.bold(s.cyan(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}

	table := tablewriter.NewWriter(w)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	// Title()/ToUpper would turn SGR "\033[1m" into "\033[1M" (CSI Delete Line) and erase headers.
	table.SetAutoFormatHeaders(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetTablePadding("\t")
	table.SetHeader([]string{
		cell(s, s.bold, "KEY"),
		cell(s, s.bold, "VALUE"),
	})
	table.SetColumnAlignment([]int{
		tablewriter.ALIGN_LEFT,
		tablewriter.ALIGN_LEFT,
	})
	for _, tag := range view.Tags {
		table.Append([]string{tag.Key, tag.Value})
	}
	table.Render()
	return nil
}

func formatAccountTagTarget(view AccountTagsView) string {
	if view.Alias == "" || view.Alias == view.AccountID {
		return view.AccountID
	}
	return fmt.Sprintf("%s (%s)", view.Alias, view.AccountID)
}
