//go:build linux

package collector

import "testing"

func TestParseMDStat(t *testing.T) {
	fixture := `Personalities : [raid1]
md127 : active raid1 sdb1[1] sda1[0]
      1046528 blocks super 1.2 [2/2] [UU]

md0 : active raid5 sdd1[3] sdc1[2] sdb1[1] sda1[0]
      182272 blocks [4/3] [U_UU]

unused devices: <none>`
	a := ParseMDStat(fixture)
	if len(a) != 2 || a[0].Status != "active" || a[1].Status != "degraded" {
		t.Fatalf("unexpected array states %+v", a)
	}
}

func TestMDArrayInventoryAndMemberRoles(t *testing.T) {
	fixture := `md0 : active raid1 sdb1[2] sda1[0] sdc1[3](S) sdd1[1](F)
      1046528 blocks super 1.2 [2/1] [U_]
      [==>..................] recovery = 12.5% (10/80) finish=1.0min
md1 : inactive sde[0](S)
      512 blocks super external:imsm
`
	i := parseMD(fixture, "")
	checkInventory(t, i)
	if len(i.Arrays) != 1 || len(i.Drives) != 4 {
		t.Fatalf("unexpected MD inventory: %+v", i)
	}
	a := i.Arrays[0]
	if a.Level != "RAID1" || a.Status != "rebuilding" || a.Expected != 2 || a.Missing != 1 ||
		a.Progress == nil || *a.Progress != 12.5 || len(a.Members) != 4 ||
		i.Drives[2].Role != "spare" || i.Drives[3].Status != "inactive" {
		t.Fatalf("invalid MD attributes: %+v", i)
	}
}
