package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/openshift-online/finops-tools/core/accountreview"
)

var accountDetailsCSVHeader = []string{
	"account_id", "account_name", "owner_email", "section",
	"id", "name", "attr1", "attr2", "attr3", "attr4", "attr5", "amount", "currency",
}

// WriteAccountDetails writes published account-review details in the requested format.
func WriteAccountDetails(w io.Writer, format Format, details []accountreview.AccountDetails) error {
	if details == nil {
		details = []accountreview.AccountDetails{}
	}
	switch format {
	case FormatPrettyPrint:
		return writeAccountDetailsPretty(w, details)
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(details)
	case FormatCSV:
		return writeAccountDetailsCSV(w, details)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

func writeAccountDetailsCSV(w io.Writer, details []accountreview.AccountDetails) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(accountDetailsCSVHeader); err != nil {
		return err
	}
	for _, d := range details {
		if err := writeAccountDetailsCSVAccount(cw, d); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeAccountDetailsCSVAccount(cw *csv.Writer, d accountreview.AccountDetails) error {
	if err := cw.Write(accountDetailsCSVRow(d, accountreview.SectionAccount, d.AccountID, d.AccountName, d.DisplayAlias, d.OUPath, "", "", "", "", "")); err != nil {
		return err
	}
	if d.OwnerError != "" {
		if err := cw.Write(accountDetailsCSVRow(d, accountreview.SectionOwnerError, "", "", d.OwnerError, "", "", "", "", "", "")); err != nil {
			return err
		}
	}
	for _, tag := range d.Tags {
		if err := cw.Write(accountDetailsCSVRow(d, accountreview.SectionTag, "", tag.Key, tag.Value, "", "", "", "", "", "")); err != nil {
			return err
		}
	}
	mc := d.MonthlyCosts
	if mc.Error != "" {
		if err := cw.Write(accountDetailsCSVRow(d, accountreview.SectionMonth, "", "", mc.Error, "", "", "", "", "", mc.Currency)); err != nil {
			return err
		}
	} else {
		for _, m := range mc.Months {
			if err := cw.Write(accountDetailsCSVRow(d, accountreview.SectionMonth, "", m.Month, "", "", "", "", "", csvAmount(m.Amount), mc.Currency)); err != nil {
				return err
			}
		}
		if len(mc.Months) > 0 {
			if err := cw.Write(accountDetailsCSVRow(d, accountreview.SectionMonth, "", "Total", "", "", "", "", "", csvAmount(mc.Total), mc.Currency)); err != nil {
				return err
			}
		}
	}
	if mc.Error == "" && len(mc.TopServices) > 0 {
		for _, svc := range mc.TopServices {
			if err := cw.Write(accountDetailsCSVRow(d, accountreview.SectionTopService, "", svc.Service, "", "", "", "", "", csvAmount(svc.Amount), mc.Currency)); err != nil {
				return err
			}
		}
	} else if mc.Error == "" && mc.TopServicesError != "" {
		if err := cw.Write(accountDetailsCSVRow(d, accountreview.SectionTopService, "", "", mc.TopServicesError, "", "", "", "", "", mc.Currency)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(d.InventoryError) != "" {
		if err := cw.Write(accountDetailsCSVRow(d, accountreview.SectionInventoryError, "", "", d.InventoryError, "", "", "", "", "", "")); err != nil {
			return err
		}
	}
	if strings.TrimSpace(d.OpenShiftClustersError) != "" {
		if err := cw.Write(accountDetailsCSVRow(d, accountreview.SectionOpenShiftError, "", "", d.OpenShiftClustersError, "", "", "", "", "", "")); err != nil {
			return err
		}
	}
	for _, table := range d.InventoryTables() {
		for _, row := range table.Rows {
			id, name, attr1, attr2, attr3, attr4, attr5 := csvCellsFromResourceRow(row)
			if err := cw.Write(accountDetailsCSVRow(d, table.Key, id, name, attr1, attr2, attr3, attr4, attr5, "", "")); err != nil {
				return err
			}
		}
	}
	for _, c := range csvInventoryCounts(d) {
		if err := cw.Write(accountDetailsCSVRow(d, accountreview.SectionCount, c.Key, c.Title, strconv.Itoa(c.Count), "", "", "", "", "", "")); err != nil {
			return err
		}
	}
	// OpenShift CSV uses ClusterID as id (like ec2/rds), not the display table
	// order which leads with Environment for humans.
	for _, c := range d.OpenShiftClusters {
		if err := cw.Write(accountDetailsCSVRow(
			d,
			accountreview.SectionOpenShift,
			c.ClusterID,
			c.Name,
			c.Environment,
			c.ProductType,
			c.State,
			c.Region,
			c.OpenShiftVersion,
			"",
			"",
		)); err != nil {
			return err
		}
	}
	return nil
}

// csvInventoryCounts omits zeros when the scan was incomplete so CSV consumers
// cannot treat missing regions as confirmed-empty inventory.
func csvInventoryCounts(d accountreview.AccountDetails) []accountreview.ResourceCount {
	if strings.TrimSpace(d.InventoryError) != "" {
		return d.NonzeroInventoryCounts()
	}
	return d.InventoryCounts()
}

func accountDetailsCSVRow(d accountreview.AccountDetails, section, id, name, attr1, attr2, attr3, attr4, attr5, amount, currency string) []string {
	return []string{
		sanitizeCSVField(d.AccountID),
		sanitizeCSVField(d.AccountName),
		sanitizeCSVField(d.OwnerEmail),
		section,
		sanitizeCSVField(id),
		sanitizeCSVField(name),
		sanitizeCSVField(attr1),
		sanitizeCSVField(attr2),
		sanitizeCSVField(attr3),
		sanitizeCSVField(attr4),
		sanitizeCSVField(attr5),
		amount,
		sanitizeCSVField(currency),
	}
}

func csvCellsFromResourceRow(row []string) (id, name, attr1, attr2, attr3, attr4, attr5 string) {
	switch len(row) {
	case 7:
		return row[0], row[1], row[2], row[3], row[4], row[5], row[6]
	case 5:
		return row[0], row[1], row[2], row[3], row[4], "", ""
	case 4:
		return row[0], row[1], row[2], row[3], "", "", ""
	case 3:
		return "", row[0], row[1], row[2], "", "", ""
	case 2:
		return "", row[0], row[1], "", "", "", ""
	case 1:
		return "", row[0], "", "", "", "", ""
	default:
		return "", "", "", "", "", "", ""
	}
}

func csvAmount(amount float64) string {
	return strconv.FormatFloat(amount, 'f', -1, 64)
}
