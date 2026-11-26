package main

import (
 "bytes"
 "encoding/json"
 "io"
 "log"
 "net/http"
 "time"
 "github.com/iPapik/Storavix/internal/collector"
 "github.com/iPapik/Storavix/internal/telemetry"
)
func runLocateWorker(client *http.Client,base,token,id string) {
 // No redirects: the shared registration credential must stay with the configured server.
 c:=*client
 c.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
 var result *telemetry.LocateResult
 for {
  payload,_:=json.Marshal(telemetry.LocatePoll{AgentID:id,Result:result})
  req,err:=http.NewRequest(http.MethodPost,base+"/api/agent/locate",bytes.NewReader(payload))
  if err==nil {
   req.Header.Set("Authorization","Bearer "+token)
   req.Header.Set("X-Storavix-Request","1")
   req.Header.Set("Content-Type","application/json")
   resp,e:=c.Do(req)
   if e==nil {
    if resp.StatusCode==http.StatusOK {
     var body struct{Command *telemetry.LocateCommand `json:"command"`}
     e=json.NewDecoder(io.LimitReader(resp.Body,8192)).Decode(&body)
     resp.Body.Close()
     if e==nil {
      result=nil
      if cmd:=body.Command;cmd!=nil {
       err:=collector.LocateDrive(*cmd)
       result=&telemetry.LocateResult{ID:cmd.ID,OK:err==nil,Message:"Контроллер подтвердил команду Locate"}
       if err!=nil {result.Message=err.Error()}
       log.Printf("locate id=%s action=%s result=%s",cmd.ID,cmd.Action,result.Message)
       continue
      }
     }
    } else {resp.Body.Close()}
   }
  }
  time.Sleep(5*time.Second)
 }
}
