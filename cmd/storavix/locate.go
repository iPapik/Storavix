package main

import (
 "crypto/subtle"
 "encoding/hex"
 "log"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "time"
 "github.com/iPapik/Storavix/internal/collector"
 "github.com/iPapik/Storavix/internal/telemetry"
)
type locateRecord struct {
 telemetry.LocateCommand
 Status string `json:"status"`
 Message string `json:"message"`
 UpdatedAt time.Time `json:"updatedAt"`
}
func (s *server) locateCurrentLocked(id string,now time.Time) locateRecord {
 c:=s.locateCommands[id]
 if c.Status=="queued" && now.Sub(c.UpdatedAt)>time.Minute {
  c.Status,c.Message="expired","Агент не получил команду вовремя"
  s.locateCommands[id]=c
 } else if c.Status=="running" && now.Sub(c.UpdatedAt)>time.Minute {
  c.Status,c.Message="unknown","Нет подтверждения контроллера; проверьте LED физически"
  s.locateCommands[id]=c
 }
 return c
}
func (s *server) requestLocate(w http.ResponseWriter,r *http.Request) {
 var in struct {DriveID string `json:"driveId"`; Serial string `json:"serial"`; Action string `json:"action"`}
 if decodeJSON(r,&in)!=nil || (in.Action!="start" && in.Action!="stop") || in.Serial=="" {problem(w,400,"Неверная команда Locate");return}
 id:=r.PathValue("id")
 now:=time.Now().UTC()
 s.mu.Lock()
 defer s.mu.Unlock()
 report,ok:=s.agents[id]
 if !ok {problem(w,404,"Сервер не найден");return}
 if report.OS!="windows" || now.Sub(s.locatePolls[id])>30*time.Second {problem(w,409,"Требуется работающий Windows-агент с поддержкой Locate (v0.2.25+)");return}
 if report.Storage==nil || now.Sub(report.ReceivedAt)>2*time.Minute || now.Sub(report.Storage.CollectedAt)>4*time.Minute {
  problem(w,409,"Данные дисков устарели. Дождитесь нового опроса");return
 }
 current:=s.locateCurrentLocked(id,now)
 if current.Status=="queued" || current.Status=="running" {problem(w,409,"На сервере уже выполняется Locate");return}
 var target telemetry.LocateCommand
 matches:=0
 for _,d:=range report.Storage.Drives {
  if d.ID!=in.DriveID || d.Serial!=in.Serial {continue}
  for _,c:=range report.Storage.Controllers {
   if c.ID==d.Controller {
    matches++
    target=telemetry.LocateCommand{ID:hex.EncodeToString(randomBytes(16)),Action:in.Action,Controller:c.ID,ControllerSerial:c.Serial,Source:c.Source,Slot:d.Slot,Serial:d.Serial}
   }
  }
 }
 if matches!=1 {problem(w,409,"Диск не найден или его серийный номер изменился");return}
 if _,_,err:=collector.LocateTarget(target);err!=nil {problem(w,400,err.Error());return}
 if s.locateCommands==nil {s.locateCommands=make(map[string]locateRecord)}
 c:=locateRecord{LocateCommand:target,Status:"queued",Message:"Команда ожидает агента",UpdatedAt:now}
 s.locateCommands[id]=c
 log.Printf("locate requested agent=%s controller=%s slot=%s action=%s user=%s id=%s",id,target.Controller,target.Slot,target.Action,requestAccount(r).Username,target.ID)
 sendJSON(w,202,c)
}
func (s *server) getLocate(w http.ResponseWriter,r *http.Request) {
 s.mu.Lock()
 defer s.mu.Unlock()
 id:=r.PathValue("id")
 if _,ok:=s.agents[id];!ok {problem(w,404,"Сервер не найден");return}
 sendJSON(w,200,s.locateCurrentLocked(id,time.Now().UTC()))
}
func (s *server) pollLocate(w http.ResponseWriter,r *http.Request) {
 h:=r.Header.Get("Authorization")
 expected,err:=os.ReadFile(filepath.Join(s.dataDir,"agent.token"))
 if err!=nil || !strings.HasPrefix(h,"Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h,"Bearer ")),[]byte(strings.TrimSpace(string(expected))))!=1 {
  problem(w,401,"invalid agent token");return
 }
 var in telemetry.LocatePoll
 if decodeJSONLimit(r,&in,8192)!=nil || len(in.AgentID)!=32 {problem(w,400,"invalid locate poll");return}
 now:=time.Now().UTC()
 s.mu.Lock()
 defer s.mu.Unlock()
 report,ok:=s.agents[in.AgentID]
 if !ok || report.OS!="windows" {problem(w,404,"agent not registered");return}
 if s.locatePolls==nil {s.locatePolls=make(map[string]time.Time)}
 s.locatePolls[in.AgentID]=now
 c:=s.locateCurrentLocked(in.AgentID,now)
 if result:=in.Result;result!=nil && result.ID==c.ID && (c.Status=="running" || c.Status=="unknown") {
  c.Status="failed"
  if result.OK {c.Status="succeeded"}
  c.Message=result.Message
  if len(c.Message)>350 {c.Message=c.Message[:350]}
  c.UpdatedAt=now
  s.locateCommands[in.AgentID]=c
  log.Printf("locate finished agent=%s id=%s status=%s",in.AgentID,c.ID,c.Status)
 }
 var command *telemetry.LocateCommand
 if c.Status=="queued" {
  c.Status,c.Message,c.UpdatedAt="running","Агент проверяет диск и выполняет команду",now
  s.locateCommands[in.AgentID]=c
  command=&c.LocateCommand
 }
 sendJSON(w,200,struct{Command *telemetry.LocateCommand `json:"command"`}{command})
}
