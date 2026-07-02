// report_list.go implements "finops report list".
package cmd

import (
	reportpkg "github.com/openshift-online/finops-tools/cli/internal/report"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/spf13/cobra"
)

var reportListFormat string
var reportListOutput string

var reportListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available report templates",
	Long: `List report templates that can be passed as the first argument to "finops report create".

Example:
  finops report list
  finops report list --format json`,
	Args: cobra.NoArgs,
	PreRunE: func(_ *cobra.Command, _ []string) error {
		_, err := output.ParseFormat(reportListFormat)
		return err
	},
	RunE: runReportList,
}

func init() {
	reportCmd.AddCommand(reportListCmd)
	reportListCmd.Flags().StringVar(&reportListFormat, "format", string(output.FormatPrettyPrint),
		"Output format: pretty-print, json, csv")
	addOutputFlag(reportListCmd, &reportListOutput)
}

func runReportList(cmd *cobra.Command, _ []string) error {
	format, err := output.ParseFormat(reportListFormat)
	if err != nil {
		return err
	}
	out, closeOut, err := resolveCommandOutput(cmd, reportListOutput)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}
	return output.WriteReportTemplateList(out, format, reportpkg.Templates())
}
