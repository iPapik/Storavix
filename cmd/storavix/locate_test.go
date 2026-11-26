package main

import (
 "encoding/json"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
 "github.com/iPapik/Storavix/internal/telemetry"
)
func TestLocateDispatchExactlyOnce(t *testing.T) {
 id:=strings.Repeat("a",32)
 now:=time.Now()
 token:=strings.Repeat("b",64)
 s:=&server{dataDir:t.TempDir(),agents:map[string]telemetry.Report{id:{ID:id,OS:"windows",ReceivedAt:now,Storage:&telemetry.Storage{CollectedAt:now,Controllers:[]telemetry.Controller{{ID:"mega:/c0",Source:"storcli",Serial:"CTRL"}},Drives:[]telemetry.Drive{{ID:"mega:/c0/252:4",Controller:"mega:/c0",Slot:"252:4",Serial:"DISK"}}}}},locatePolls:map[string]time.Time{id:now}}
 if err:=os.WriteFile(filepath.Join(s.dataDir,"agent.token"),[]byte(token),0600);err!=nil {t.Fatal(err)}
 request:=func(serial string)*httptest.ResponseRecorder{
  r:=httptest.NewRequest("POST","/api/agents/"+id+"/locate",strings.NewReader(`{"driveId":"mega:/c0/252:4","serial":"`+serial+`","action":"start"}`))
  r.SetPathValue("id",id)
  w:=httptest.NewRecorder();s.requestLocate(w,r);return w
 }
 if w:=request("old");w.Code!=409 {t.Fatal(w.Code,w.Body.String())}
 if w:=request("DISK");w.Code!=202 {t.Fatal(w.Code,w.Body.String())}
 if w:=request("DISK");w.Code!=409 {t.Fatal("duplicate queued",w.Code)}
 poll:=func(body,tok string)*httptest.ResponseRecorder{
  r:=httptest.NewRequest("POST","/api/agent/locate",strings.NewReader(body));r.Header.Set("Authorization","Bearer "+tok)
  w:=httptest.NewRecorder();s.pollLocate(w,r);return w
 }
 body:=`{"agentId":"`+id+`"}`
 if w:=poll(body,"bad");w.Code!=401 {t.Fatal(w.Code)}
 w:=poll(body,token)
 var got struct{Command *telemetry.LocateCommand `json:"command"`}
 if err:=json.Unmarshal(w.Body.Bytes(),&got);err!=nil || got.Command==nil || got.Command.Serial!="DISK" {t.Fatal(w.Body.String(),err)}
 if w:=poll(body,token);!strings.Contains(w.Body.String(),`"command":null`) {t.Fatal("replayed command",w.Body.String())}
 w=poll(`{"agentId":"`+id+`","result":{"id":"`+got.Command.ID+`","ok":true,"message":"confirmed"}}`,token)
 if w.Code!=200 || s.locateCommands[id].Status!="succeeded" {t.Fatal(w.Code,s.locateCommands[id])}
}
func TestLocateExpires(t *testing.T){
 s:=&server{locateCommands:map[string]locateRecord{"a":{Status:"queued",UpdatedAt:time.Now().Add(-2*time.Minute)}}}
 if s.locateCurrentLocked("a",time.Now()).Status!="expired" {t.Fatal("stale command not expired")}
}
