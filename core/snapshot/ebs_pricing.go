package snapshot

import (
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ectypes "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type ebsSnapshotCharge struct {
	IncrementalGiB float64
	MonthlyUSD     float64
}

// computeEBSSnapshotCharges estimates per-snapshot incremental storage and monthly cost.
// Snapshots that add no new blocks in their volume chain get zero cost (deleting them
// would not reduce monthly spend). Snapshots without a volume ID are billed standalone.
func computeEBSSnapshotCharges(snapshots []ectypes.Snapshot) map[string]ebsSnapshotCharge {
	byVolume := make(map[string][]ectypes.Snapshot)
	var orphans []ectypes.Snapshot
	for _, snap := range snapshots {
		volumeID := strings.TrimSpace(aws.ToString(snap.VolumeId))
		if volumeID == "" {
			orphans = append(orphans, snap)
			continue
		}
		byVolume[volumeID] = append(byVolume[volumeID], snap)
	}

	charges := make(map[string]ebsSnapshotCharge, len(snapshots))
	for _, volumeSnaps := range byVolume {
		sort.Slice(volumeSnaps, func(i, j int) bool {
			return aws.ToTime(volumeSnaps[i].StartTime).Before(aws.ToTime(volumeSnaps[j].StartTime))
		})
		var prevFullGiB float64
		for _, snap := range volumeSnaps {
			snapID := aws.ToString(snap.SnapshotId)
			if snapID == "" {
				continue
			}
			fullGiB := ebsFullSnapshotSizeGiB(snap)
			var incrementalGiB float64
			if ebsSnapshotIsArchive(snap) {
				incrementalGiB = fullGiB
			} else {
				incrementalGiB = fullGiB - prevFullGiB
				if incrementalGiB < 0 {
					incrementalGiB = 0
				}
			}
			if fullGiB > prevFullGiB {
				prevFullGiB = fullGiB
			}
			charges[snapID] = ebsSnapshotCharge{
				IncrementalGiB: incrementalGiB,
				MonthlyUSD:     ebsIncrementalMonthlyUSD(incrementalGiB, snap),
			}
		}
	}
	for _, snap := range orphans {
		snapID := aws.ToString(snap.SnapshotId)
		if snapID == "" {
			continue
		}
		fullGiB := ebsFullSnapshotSizeGiB(snap)
		charges[snapID] = ebsSnapshotCharge{
			IncrementalGiB: fullGiB,
			MonthlyUSD:     ebsIncrementalMonthlyUSD(fullGiB, snap),
		}
	}
	return charges
}

func ebsSnapshotIsArchive(snap ectypes.Snapshot) bool {
	return strings.EqualFold(strings.TrimSpace(string(snap.StorageTier)), "archive")
}

func ebsFullSnapshotSizeGiB(snap ectypes.Snapshot) float64 {
	if bytes := snap.FullSnapshotSizeInBytes; bytes != nil && *bytes > 0 {
		return float64(*bytes) / (1024 * 1024 * 1024)
	}
	return float64(aws.ToInt32(snap.VolumeSize))
}

func ebsIncrementalMonthlyUSD(incrementalGiB float64, snap ectypes.Snapshot) float64 {
	if incrementalGiB <= 0 {
		return 0
	}
	tier := strings.TrimSpace(string(snap.StorageTier))
	return EstimateMonthlyCost(KindEBSSnapshot, incrementalGiB, tier, "")
}

func ebsRegionalMonthlyCostUSD(charges map[string]ebsSnapshotCharge) float64 {
	var total float64
	for _, charge := range charges {
		total += charge.MonthlyUSD
	}
	return total
}

func applyEBSChargeToRecord(rec *Record, charge ebsSnapshotCharge, hasCharge bool) {
	if !hasCharge {
		rec.EstimatedMonthlyCostUSD = 0
		rec.CostBasis = CostBasisEBSNoIncremental
		return
	}
	rec.EstimatedMonthlyCostUSD = charge.MonthlyUSD
	if charge.IncrementalGiB > 0 {
		rec.CostBasis = CostBasisEBSIncrementalChain
		return
	}
	rec.CostBasis = CostBasisEBSNoIncremental
}
