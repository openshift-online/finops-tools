package output

import (
	"strings"
	"unicode/utf8"

	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/accountreview"
	"github.com/openshift-online/finops-tools/core/cost"
)

// SanitizeTerminal strips C0/C1 controls and ANSI/OSC escape sequences from
// untrusted strings before they are written to a TTY.
func SanitizeTerminal(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == 0x1b {
			i += skipTerminalEscape(s[i:])
			continue
		}
		if r < 32 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			i += size
			continue
		}
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

func skipTerminalEscape(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch s[1] {
	case ']': // OSC: ESC ] ... BEL or ST (ESC \)
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	case '[': // CSI: ESC [ ... final byte @-~
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)
	default:
		return 2
	}
}

func sanitizeAccountDetailsForTTY(d accountreview.AccountDetails) accountreview.AccountDetails {
	d.AccountID = SanitizeTerminal(d.AccountID)
	d.AccountName = SanitizeTerminal(d.AccountName)
	d.DisplayAlias = SanitizeTerminal(d.DisplayAlias)
	d.OUPath = SanitizeTerminal(d.OUPath)
	d.OwnerEmail = SanitizeTerminal(d.OwnerEmail)
	d.OwnerError = SanitizeTerminal(d.OwnerError)
	d.InventoryError = SanitizeTerminal(d.InventoryError)
	d.OpenShiftClustersError = SanitizeTerminal(d.OpenShiftClustersError)
	d.MonthlyCosts = sanitizeMonthlyCostsForTTY(d.MonthlyCosts)
	if n := len(d.Tags); n > 0 {
		tags := make([]coreaccount.Tag, n)
		for i, tag := range d.Tags {
			tags[i] = coreaccount.Tag{Key: SanitizeTerminal(tag.Key), Value: SanitizeTerminal(tag.Value)}
		}
		d.Tags = tags
	}
	if n := len(d.EC2Instances); n > 0 {
		rows := make([]accountreview.EC2Detail, n)
		for i, inst := range d.EC2Instances {
			rows[i] = accountreview.EC2Detail{
				InstanceID: SanitizeTerminal(inst.InstanceID),
				Name:       SanitizeTerminal(inst.Name),
				Type:       SanitizeTerminal(inst.Type),
				State:      SanitizeTerminal(inst.State),
				Region:     SanitizeTerminal(inst.Region),
			}
		}
		d.EC2Instances = rows
	}
	if n := len(d.RDSInstances); n > 0 {
		rows := make([]accountreview.RDSDetail, n)
		for i, db := range d.RDSInstances {
			rows[i] = accountreview.RDSDetail{
				InstanceID: SanitizeTerminal(db.InstanceID),
				Engine:     SanitizeTerminal(db.Engine),
				Class:      SanitizeTerminal(db.Class),
				Status:     SanitizeTerminal(db.Status),
				Region:     SanitizeTerminal(db.Region),
			}
		}
		d.RDSInstances = rows
	}
	if n := len(d.RDSClusters); n > 0 {
		rows := make([]accountreview.RDSClusterDetail, n)
		for i, c := range d.RDSClusters {
			rows[i] = accountreview.RDSClusterDetail{
				ClusterID: SanitizeTerminal(c.ClusterID),
				Engine:    SanitizeTerminal(c.Engine),
				Status:    SanitizeTerminal(c.Status),
				Region:    SanitizeTerminal(c.Region),
			}
		}
		d.RDSClusters = rows
	}
	if n := len(d.HostedZones); n > 0 {
		rows := make([]accountreview.HostedZoneDetail, n)
		for i, z := range d.HostedZones {
			rows[i] = accountreview.HostedZoneDetail{
				Name:        SanitizeTerminal(z.Name),
				Type:        SanitizeTerminal(z.Type),
				RecordCount: z.RecordCount,
			}
		}
		d.HostedZones = rows
	}
	if n := len(d.OpenShiftClusters); n > 0 {
		rows := make([]accountreview.OpenShiftClusterDetail, n)
		for i, c := range d.OpenShiftClusters {
			rows[i] = accountreview.OpenShiftClusterDetail{
				Environment:      SanitizeTerminal(c.Environment),
				Name:             SanitizeTerminal(c.Name),
				ClusterID:        SanitizeTerminal(c.ClusterID),
				ProductType:      SanitizeTerminal(c.ProductType),
				State:            SanitizeTerminal(c.State),
				Region:           SanitizeTerminal(c.Region),
				OpenShiftVersion: SanitizeTerminal(c.OpenShiftVersion),
			}
		}
		d.OpenShiftClusters = rows
	}
	return d
}

func sanitizeMonthlyCostsForTTY(mc accountreview.MonthlyCostsDetails) accountreview.MonthlyCostsDetails {
	mc.Currency = SanitizeTerminal(mc.Currency)
	mc.Error = SanitizeTerminal(mc.Error)
	mc.TopServicesError = SanitizeTerminal(mc.TopServicesError)
	if n := len(mc.Months); n > 0 {
		months := make([]cost.MonthlyCostPoint, n)
		for i, m := range mc.Months {
			months[i] = cost.MonthlyCostPoint{Month: SanitizeTerminal(m.Month), Amount: m.Amount}
		}
		mc.Months = months
	}
	if n := len(mc.TopServices); n > 0 {
		svcs := make([]accountreview.ServiceCost, n)
		for i, svc := range mc.TopServices {
			svcs[i] = accountreview.ServiceCost{Service: SanitizeTerminal(svc.Service), Amount: svc.Amount}
		}
		mc.TopServices = svcs
	}
	return mc
}
