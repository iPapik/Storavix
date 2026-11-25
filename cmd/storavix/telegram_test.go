package main

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

const testTelegramToken = "123456:abcdefghijklmnopqrstuvwxyz_123456"

func TestTelegramSettingsRedactAndPersist(t *testing.T) {
 s := &server{dataDir:t.TempDir(),key:make([]byte,32)}
 put := func(body string) int {
  w:=httptest.NewRecorder()
  s.setTelegram(w,httptest.NewRequest("PUT","/api/settings/telegram",strings.NewReader(body)))
  return w.Code
 }
 if status:=put(`{"enabled":true,"chatId":"-1001234567890","token":"`+testTelegramToken+`","alerts":["server_down"],"serverTempLimit":81}`); status!=200 {t.Fatal(status)}
 if s.state.Mail.ServerTempLimit!=81 {t.Fatal("shared threshold not saved")}
 if status:=put(`{"enabled":true,"chatId":"-1001234567890","token":"","alerts":["server_down"]}`);status!=200 {t.Fatal(status)}
 raw,err:=os.ReadFile(filepath.Join(s.dataDir,"state.json"))
 if err!=nil || strings.Contains(string(raw),testTelegramToken) {t.Fatal("plaintext token persisted",err)}
 var saved State
 if err=json.Unmarshal(raw,&saved);err!=nil {t.Fatal(err)}
 token,err:=s.decrypt(saved.EncryptedTelegramToken)
 if err!=nil || token!=testTelegramToken {t.Fatal("saved token not recovered",err)}
 w:=httptest.NewRecorder()
 s.getTelegram(w,httptest.NewRequest("GET","/api/settings/telegram",nil))
 if strings.Contains(w.Body.String(),testTelegramToken) || !strings.Contains(w.Body.String(),`"hasToken":true`) {t.Fatal("token redaction failed")}
 if put(`{"enabled":true,"chatId":"https://example.com","alerts":[]}`)!=400 {t.Fatal("invalid recipient accepted")}
 if put(`{"enabled":true,"chatId":"123","alerts":["unknown"]}`)!=400 {t.Fatal("invalid rule accepted")}
}
func TestTelegramIndependentQueue(t *testing.T) {
 s:= &server{dataDir:t.TempDir(),key:make([]byte,32)}
 s.state.Telegram=TelegramConfig{Enabled:true,ChatID:"123",Alerts:[]string{"server_down","server_up"}}
 s.state.Mail.Alerts=[]string{"server_down"}
 enc,err:=s.encrypt(testTelegramToken)
 if err!=nil {t.Fatal(err)}
 s.state.EncryptedTelegramToken=enc
 at:=time.Now()
 s.applyMonitorLocked("a","g","srv03",at,[]monitorChange{{Key:"status",Status:"down",Event:"server_down",Detail:"Нет отчётов",ReportFirst:true}})
 s.applyMonitorLocked("a","g","srv03",at,[]monitorChange{{Key:"status",Status:"down",Event:"server_down",ReportFirst:true}})
 if len(s.state.Alerts)!=1 || s.state.Alerts[0].Delivery!="queued" || s.state.Alerts[0].TelegramDelivery!="queued" {t.Fatal("incorrect channel queue or duplicate event")}
 sent:=0
 send:=func(_ context.Context,c TelegramConfig,token string,a Alert)error{
  sent++
  if token!=testTelegramToken || c.ChatID!="123" || a.Kind!="server_down" {t.Fatal("incorrect queued payload")}
  if sent==1 {return telegramRateError{7}}
  return nil
 }
 if ok,delay:=s.deliverMonitorTelegram(context.Background(),send);!ok || delay!=7*time.Second || s.state.Alerts[0].TelegramDelivery!="queued" {t.Fatal("rate limit not respected")}
 if ok,_:=s.deliverMonitorTelegram(context.Background(),send);!ok || s.state.Alerts[0].TelegramDelivery!="sent" || s.state.Alerts[0].Delivery!="queued" {t.Fatal("Telegram altered SMTP")}
 s.applyMonitorLocked("a","g","srv03",at.Add(time.Second),[]monitorChange{{Key:"status",Status:"up",Event:"server_up"}})
 if s.state.Alerts[0].Delivery!="disabled" || s.state.Alerts[0].TelegramDelivery!="queued" {t.Fatal("Telegram-only event failed")}
 s.deliverMonitorTelegram(context.Background(),func(context.Context,TelegramConfig,string,Alert)error{return errors.New("unavailable")})
 if s.state.Alerts[0].TelegramDelivery!="failed" || s.state.Alerts[0].Delivery!="disabled" {t.Fatal("failure status not isolated")}
}
func TestTelegramBotAPI(t *testing.T) {
 fixture:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Method!="POST" || !strings.HasSuffix(r.URL.Path,"/sendMessage") {t.Error("incorrect request")}
  var payload map[string]any
  if json.NewDecoder(r.Body).Decode(&payload)!=nil || payload["chat_id"]!="123" {t.Error("incorrect JSON")}
  w.Header().Set("Content-Type","application/json")
  w.WriteHeader(403)
  _,_=w.Write([]byte(`{"ok":false,"error_code":403,"description":"`+testTelegramToken+`"}`))
 }))
 defer fixture.Close()
 _,err:=telegramCall(context.Background(),fixture.Client(),fixture.URL,testTelegramToken,"sendMessage",map[string]any{"chat_id":"123","text":"test"})
 if err==nil || strings.Contains(err.Error(),testTelegramToken) {t.Fatal("API error handling leaked token",err)}
 message:=telegramText(Alert{CreatedAt:time.Now(),Subject:"srv03",Severity:"critical",Detail:strings.Repeat("😀",5000)})
 if len([]rune(message))>2000 || !strings.Contains(message,"Время события:") {t.Fatal("message limit or timestamp missing")}
}
func TestTelegramPendingSurvivesHistoryTrim(t *testing.T) {
 events:=make([]Alert,eventHistoryLimit+1)
 for i:=range events {events[i]=Alert{ID: "history",CreatedAt:time.Unix(int64(i+1),0)}}
 events[0].ID="pending"
 events[0].TelegramDelivery="queued"
 out:=trimAlerts(events)
 for _,a:=range out {if a.ID=="pending" {return}}
 t.Fatal("pending Telegram event discarded")
}
