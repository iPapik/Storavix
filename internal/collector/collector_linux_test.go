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
