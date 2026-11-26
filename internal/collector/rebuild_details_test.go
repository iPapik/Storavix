package collector

import (
 "strings"
 "testing"
)
func TestMegaRebuildDetails(t *testing.T) {
 raw := `{"Controllers":[{"Command Status":{"Controller":0,"Status":"Success"},"Response Data":{"Drive /c0/e252/s4":[{"Drive-ID":"/c0/e252/s4","Progress%":0,"Status":"In progress","Estimated Time Left":"2 Hours 15 Minutes"},{"Drive-ID":"/c0/e252/s5","Progress%":"35","Status":"In progress","Estimated Time Left":"-"},{"Drive-ID":"/c0/e252/s6","Progress%":100,"Status":"Not in progress"},{"Drive-ID":"/c1/e252/s4","Progress%":80,"Status":"In progress"}]}}]}`
 rows,_:=decodeMega([]byte(raw))
 got:=megaRebuildDetails(rows)
 a:=got[megaKey{"/c0","252:4"}]
 b:=got[megaKey{"/c0","252:5"}]
 if len(got)!=2 || a.Progress==nil || *a.Progress!=0 || a.Remaining!="2 Hours 15 Minutes" || a.Status!="In progress" || b.Progress==nil || *b.Progress!=35 || b.Remaining!="" {t.Fatalf("incorrect rebuild details: %+v",got)}
}
func TestMegaRebuildInventory(t *testing.T){
 fixtures:=map[string]string{
 "/call show all J":`{"Controllers":[{"Command Status":{"Controller":0,"Status":"Success"},"Response Data":{"Basics":{"Model":"MegaRAID"},"PD LIST":[{"EID:Slt":"252:4","State":"Rbld","DG":0}]}}]}`,
 "/call/eall/sall show all J":`{"Controllers":[{"Command Status":{"Controller":0,"Status":"Success"},"Response Data":{"Drive /c0/e252/s4 - Detailed Information":{"Drive /c0/e252/s4 Device attributes":{"SN":"DISK"}}}}]}`,
 "/call/eall/sall show rebuild J":`{"Controllers":[{"Command Status":{"Controller":0,"Status":"Success"},"Response Data":{"Drive /c0/e252/s4":[{"Drive-ID":"/c0/e252/s4","Progress%":"54","Status":"In progress","Estimated Time Left":"35 Minutes"}]}}]}`,
 }
 inventory,status:=readMegaWithRunner("storcli",func(args ...string)[]byte{return []byte(fixtures[strings.Join(args," ")])})
 if status!="ok" || len(inventory.Drives)!=1 {t.Fatal(status,inventory)}
 d:=inventory.Drives[0]
 if d.Detail!="Rebuild" || d.Progress==nil || *d.Progress!=54 || d.RebuildRemaining!="35 Minutes" || d.RebuildStatus!="In progress" {t.Fatalf("wrong disk: %+v",d)}
 checkInventory(t,inventory)
}
func TestMegaFullState(t *testing.T){
 for raw,want:=range map[string]string{"Onln":"Online","Rbld":"Rebuild","Offln":"Offline","UGood":"Unconfigured Good","UBad":"Unconfigured Bad","GHS":"Global Hot Spare","DHS":"Dedicated Hot Spare","Msng":"Missing"} {if got:=megaFullState(raw);got!=want {t.Errorf("%s = %s, want %s",raw,got,want)}}
}
