package collector

import (
 "errors"
 "strings"
 "testing"
 "github.com/iPapik/Storavix/internal/telemetry"
)
func locateFixture() telemetry.LocateCommand {
 return telemetry.LocateCommand{Action:"start",Controller:"mega:/c0",ControllerSerial:"CTRL",Source:"storcli",Slot:"252:4",Serial:"DISK123"}
}
const locateControllerJSON = `{"Controllers":[{"Command Status":{"Controller":0,"Status":"Success"},"Response Data":{"Basics":{"Serial Number":"CTRL"}}}]}`
const locateDiskJSON = `{"Controllers":[{"Command Status":{"Controller":0,"Status":"Success"},"Response Data":{"Drive /c0/e252/s4 - Detailed Information":{"Drive /c0/e252/s4 Device attributes":{"SN":"DISK123"}}}}]}`
const locateOK = `{"Controllers":[{"Command Status":{"Controller":0,"Status":"Success"}}]}`
func TestLocateStrictTarget(t *testing.T) {
 c:=locateFixture()
 _,path,err:=LocateTarget(c)
 if err!=nil || path!="/c0/e252/s4" {t.Fatal(path,err)}
 for _,slot:=range []string{"all","252:4-7","ID 4","252:s4","252:4 /s5","-:4"} {
  c.Slot=slot
  if _,_,err:=LocateTarget(c);err==nil {t.Fatal("accepted ambiguous slot",slot)}
 }
 c=locateFixture();c.Serial=""
 if _,_,err:=LocateTarget(c);err==nil {t.Fatal("accepted missing serial")}
 c=locateFixture();c.Action="offline"
 if _,_,err:=LocateTarget(c);err==nil {t.Fatal("accepted destructive command")}
}
func TestLocateVerifiesIdentityAndOnlyUsesLED(t *testing.T) {
 for _,action:=range []string{"start","stop"} {
  c:=locateFixture();c.Action=action
  calls:=0
  err:=locateWithRunner(c,func(args ...string)([]byte,error){
   calls++
   switch strings.Join(args," ") {
   case "/c0 show all J":return []byte(locateControllerJSON),nil
   case "/c0/e252/s4 show all J":return []byte(locateDiskJSON),nil
   case "/c0/e252/s4 "+action+" locate J":return []byte(locateOK),nil
   default:t.Fatal("unexpected CLI arguments",args);return nil,errors.New("unexpected")
   }
  })
  if err!=nil || calls!=3 {t.Fatal(err,calls)}
 }
 c:=locateFixture()
 calls:=0
 err:=locateWithRunner(c,func(args ...string)([]byte,error){
  calls++
  if calls==1{return []byte(locateControllerJSON),nil}
  if calls==2{return []byte(strings.ReplaceAll(locateDiskJSON,"DISK123","REPLACEMENT")),nil}
  t.Fatal("command executed despite identity mismatch");return nil,nil
 })
 if err==nil || calls!=2 {t.Fatal("serial mismatch not rejected")}
}
func TestLocateRejectsCLIFailure(t *testing.T) {
 c:=locateFixture()
 calls:=0
 err:=locateWithRunner(c,func(args ...string)([]byte,error){
  calls++
  if calls==1{return []byte(locateControllerJSON),nil}
  if calls==2{return []byte(locateDiskJSON),nil}
  return []byte(strings.ReplaceAll(locateOK,"Success","Failure")),nil
 })
 if err==nil {t.Fatal("false success")}
}
