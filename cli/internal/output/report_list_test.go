package output

import (
	"bytes"
	"strings"
	"testing"

	reportpkg "github.com/openshift-online/finops-tools/cli/internal/report"
)

func TestWriteReportTemplateList(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteReportTemplateList(&buf, FormatPrettyPrint, reportpkg.Templates()); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	for _, want := range []string{"costs", "html", "TEMPLATE"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
