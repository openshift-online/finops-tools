// account_list_ous_pretty.go renders OUs as a colorized table.
package output

import (
	"fmt"
	"io"

	"github.com/olekukonko/tablewriter"
)

func writeOrganizationalUnitListPretty(w io.Writer, view OrganizationalUnitListView) error {
	s := newStyler(w)

	payerLabel := "Payer:"
	if s.enabled {
		payerLabel = s.dim(payerLabel)
	}
	payerValue := view.PayerAlias
	if s.enabled {
		payerValue = s.bold(s.cyan(payerValue))
	}
	if _, err := fmt.Fprintf(w, "  %s  %s\n", payerLabel, payerValue); err != nil {
		return err
	}

	if view.ParentID != "" {
		parentLabel := "Parent:"
		parentValue := view.ParentID
		if s.enabled {
			parentLabel = s.dim(parentLabel)
			parentValue = s.bold(parentValue)
		}
		if _, err := fmt.Fprintf(w, "  %s  %s\n", parentLabel, parentValue); err != nil {
			return err
		}
	}

	if len(view.Units) == 0 {
		msg := "No organizational units found."
		if s.enabled {
			msg = s.dim(msg)
		}
		_, err := fmt.Fprintln(w, msg)
		return err
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	title := "Organizational units"
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
		cell(s, s.bold, "NAME"),
		cell(s, s.bold, "OU ID"),
	})
	table.SetColumnAlignment([]int{
		tablewriter.ALIGN_LEFT,
		tablewriter.ALIGN_LEFT,
	})
	for _, ou := range view.Units {
		name := ou.Name
		if name == "" {
			name = "-"
			if s.enabled {
				name = s.dim(name)
			}
		} else if s.enabled {
			name = s.bold(name)
		}
		table.Append([]string{name, ou.ID})
	}
	table.Render()
	return nil
}
