package cmd

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestSkipRegionsFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
		fail bool
	}{
		{name: "omitted"},
		{name: "one", args: []string{"--skip-regions", "us-west-2"}, want: []string{"us-west-2"}},
		{name: "comma and repeat", args: []string{"--skip-regions", "us-west-2, us-east-1", "--skip-regions", "us-west-2,eu-west-1"}, want: []string{"eu-west-1", "us-east-1", "us-west-2"}},
		{name: "empty", args: []string{"--skip-regions="}, fail: true},
		{name: "blank", args: []string{"--skip-regions", " , "}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			var regions []string
			bindSkipRegionsFlag(cmd, &regions)
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			got, err := skipRegionsForCommand(cmd, regions)
			if (err != nil) != tc.fail || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("regions=%v err=%v", got, err)
			}
		})
	}
}

func TestResourceScanCommandsHaveSkipRegionsFlag(t *testing.T) {
	for _, cmd := range []*cobra.Command{snapshotListCmd, extendedSupportListCmd, accountDetailsCmd} {
		flag := cmd.Flags().Lookup("skip-regions")
		if flag == nil || flag.Value.Type() != "stringSlice" {
			t.Fatalf("%s: skip-regions flag missing or not repeatable", cmd.CommandPath())
		}
	}
}
