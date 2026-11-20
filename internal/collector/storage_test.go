package collector

import (
	"testing"

	"github.com/iPapik/Storavix/internal/telemetry"
)

func checkInventory(t *testing.T, inventory Inventory) {
	t.Helper()
	report := telemetry.Report{ID: "0123456789abcdef0123456789abcdef", Hostname: "srv01"}
	inventory.bounded().AddTo(&report)
	if err := report.Validate(); err != nil {
		t.Fatalf("invalid report: %v, %+v", err, report)
	}
}

func TestMegaRAIDUsesControllerAndDriveGroup(t *testing.T) {
	base, status := decodeMega([]byte(`{"Controllers":[
	    {"Command Status":{"Controller":0,"Status":"Success"},"Response Data":{
	        "Basics":{"Model":"MegaRAID 9560","Serial Number":"ABC"},"Status":{"Controller Status":"Optimal"},
	        "VD LIST":[{"DG/VD":"0/237","TYPE":"RAID5","State":"Dgrd","Name":"Data","Size":"1.2 TiB"},
	                   {"DG/VD":"1/238","TYPE":"RAID1","State":"Optl","Name":"System","Size":"931 GiB"}],
	        "PD LIST":[
	            {"EID:Slt":"251:0","State":"Onln","DG":0,"Model":"Drive A","Size":"931 GiB","Intf":"SAS","Med":"HDD"},
	            {"EID:Slt":"251:1","State":"Rbld","DG":"0","Model":"Drive B","Size":"931 GiB"},
	            {"EID:Slt":"251:2","State":"GHS","DG":"-","Model":"Global spare","Size":"931 GiB"},
	            {"EID:Slt":"251:3","State":"Onln","DG":1,"Model":"Drive C","Size":"931 GiB"}
	        ],
	        "TOPOLOGY":[{"DG":0,"Row":"-","State":"Dgrd"},{"DG":0,"Row":0,"State":"Onln"},
	                    {"DG":0,"Row":1,"State":"Rbld"},{"DG":0,"Row":2,"State":"Msng"}]
	    }},
	    {"Command Status":{"Controller":1,"Status":"Success"},"Response Data":{
	        "Basics":{"Model":"PERC"},"VD LIST":[{"DG/VD":"0/0","TYPE":"RAID0","State":"Optl"}],
	        "PD LIST":[{"EID:Slt":"251:0","State":"Onln","DG":0}]
	    }},
	    {"Command Status":{"Controller":2,"Status":"Failure","Description":"Access denied"}}
	]}`))
	if status != "partial" || len(base) != 2 {
		t.Fatalf("%s %v", status, base)
	}
	detailed, _ := decodeMega([]byte(`{"Controllers":[{"Command Status":{"Controller":0,"Status":"Success"},"Response Data":{
	  "Drive /c0/e251/s0 - Detailed Information":{
	    "Drive /c0/e251/s0 Device attributes":{"SN":" SERIAL ","Firmware Revision":"FW"},
	    "Drive /c0/e251/s0 State":{"Drive Temperature":"34C (93.2 F)","Predictive Failure Count":1}
	  }
	}}]}`))
	rebuild, _ := decodeMega([]byte(`{"Controllers":[{"Command Status":{"Controller":0,"Status":"Success"},"Response Data":{
	  "Drive /c0/e251/s1":[{"Drive-ID":"/c0/e251/s1 rebuild status","Status":"In progress","Progress%":35}]
	}}]}`))
	i := parseMega("storcli", base, megaDetails(detailed), megaProgress(rebuild))
	checkInventory(t, i)
	if len(i.Controllers) != 2 || len(i.Drives) != 5 || len(i.Arrays) != 3 {
		t.Fatalf("incomplete inventory: %+v", i)
	}
	a := i.Arrays[0]
	if a.Level != "RAID5" || a.Status != "rebuilding" || a.Progress == nil || *a.Progress != 35 ||
		a.Expected != 3 || a.Missing != 1 || len(a.Members) != 2 || a.Members[0] != "mega:/c0/251:0" ||
		len(i.Arrays[2].Members) != 1 || i.Arrays[2].Members[0] != "mega:/c1/251:0" {
		t.Fatalf("incorrect array membership or progress: %+v", i.Arrays)
	}
	d := i.Drives[0]
	if d.Serial != "SERIAL" || d.Status != "degraded" || d.SizeBytes != 931*1024*1024*1024 ||
		d.Temperature == nil || *d.Temperature != 34 || i.Drives[2].Role != "global-spare" {
		t.Fatalf("incorrect drive details: %+v", i.Drives)
	}
}

func TestHPELogicalDriveHeaderAndDiskScope(t *testing.T) {
	fixture := `Smart Array P408i-a SR Gen10 in Slot 0 (Embedded)
   Serial Number: CONTROLLER
   Firmware Version: 7.0
   Controller Status: OK
   Array: A
      Logical Drive: 1
         Fault Tolerance: 1+0
         Size: 1.82 TB
         Status: Recovering, 25% complete
         Mirror Group 1:
            physicaldrive 1I:1:1 (port 1I:box 1:bay 1, SAS HDD, 2 TB, OK)
      physicaldrive 1I:1:1
         Status: OK
         Model: TEST ONE
         Serial Number: FIRST
         Interface Type: SAS
         Size: 2 TB
      physicaldrive 1I:1:2
         Status: Rebuilding, 20% complete
         Drive Type: Data Drive
         Serial Number: SECOND
   Unassigned
      physicaldrive 1I:1:3
         Status: OK
         Drive Type: Spare Drive
Smart Array P440 in Slot 2
   Controller Status: OK
   Array B
      Logical Drive: 1
         Fault Tolerance: 5
         Status: Failed
      physicaldrive 1I:1:1
         Status: Failed
`
	i := parseHPE(fixture)
	checkInventory(t, i)
	if len(i.Controllers) != 2 || len(i.Arrays) != 2 || len(i.Drives) != 4 ||
		i.Controllers[0].Serial != "CONTROLLER" || i.Drives[0].Serial != "FIRST" || i.Drives[2].Role != "spare" {
		t.Fatalf("invalid HPE topology: %+v", i)
	}
	a := i.Arrays[0]
	if a.Level != "RAID 1+0" || a.Status != "rebuilding" || a.Progress == nil || *a.Progress != 25 ||
		len(a.Members) != 2 || len(i.Arrays[1].Members) != 1 || i.Arrays[1].Status != "inactive" {
		t.Fatalf("invalid logical drives: %+v", i.Arrays)
	}
}

func TestMissingOrFailedMegaCLI(t *testing.T) {
	for _, tt := range []struct{ body, status string }{
		{`{"Controllers":[]}`, "ok"},
		{`{"Controllers":[{"Command Status":{"Status":"Failure","Description":"No Controller found"}}]}`, "ok"},
		{`{"Controllers":[{"Command Status":{"Controller":0,"Status":"Failure","Description":"Permission denied"}}]}`, "error"},
		{`{"Controllers":null}`, "error"},
		{"tool timed out", "error"},
	} {
		if got, status := decodeMega([]byte(tt.body)); len(got) != 0 || status != tt.status {
			t.Errorf("%q: got %v, %s; want no controllers, %s", tt.body, got, status, tt.status)
		}
	}
}

func TestStorageSpacesUsesVirtualDiskAssociation(t *testing.T) {
	i, status := parseStorageSpaces([]byte(`{"pools":[{
	  "id":"pool-1","name":"Main","operational":"OK","health":"Healthy",
	  "drives":[{"id":"disk-1","model":"One","health":"Healthy","operational":"OK","size":1000},
	            {"id":"disk-2","model":"Two","health":"Warning","operational":"Lost Communication","size":1000},
	            {"id":"disk-3","model":"Three","health":"Healthy","usage":"Hot Spare","size":1000}],
	  "arrays":[{"name":"Files","operational":"Degraded, Incomplete","health":"Warning",
	             "size":1000,"layout":"Mirror (2)","members":["disk-1","disk-2"]}]
	}]}`))
	checkInventory(t, i)
	if status != "ok" || len(i.Controllers) != 1 || len(i.Drives) != 3 || len(i.Arrays) != 1 {
		t.Fatalf("incomplete Storage Spaces: %s, %+v", status, i)
	}
	a := i.Arrays[0]
	if a.Status != "degraded" || a.Level != "Mirror (2)" || len(a.Members) != 2 ||
		a.Members[1] != i.Drives[1].ID || i.Drives[1].Status != "inactive" || i.Drives[2].Role != "spare" {
		t.Fatalf("incorrect Windows disk association: %+v", i)
	}
}

func TestStorageSpacesDistinguishesEmptyAndUnavailable(t *testing.T) {
	for _, tc := range []struct{ data, want string }{
		{`{"pools":[],"partial":false}`, "ok"},
		{`{"unavailable":true}`, "unavailable"},
		{`{"pools":null}`, "error"},
		{`{}`, "error"},
	} {
		i, status := parseStorageSpaces([]byte(tc.data))
		if status != tc.want || len(i.Controllers) != 0 || len(i.Arrays) != 0 || len(i.Drives) != 0 {
			t.Errorf("%s: got %+v, %s; want empty inventory, %s", tc.data, i, status, tc.want)
		}
	}
}
