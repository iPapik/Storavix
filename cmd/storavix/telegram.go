package main

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "log"
 "net/http"
 "regexp"
 "slices"
 "strconv"
 "strings"
 "time"
)

type TelegramConfig struct {
 Enabled bool `json:"enabled"`
 ChatID string `json:"chatId"`
 TopicID int `json:"topicId,omitempty"`
 Alerts []string `json:"alerts"`
 HasToken bool `json:"hasToken"`
}
type telegramInput struct {
 TelegramConfig
 Token string `json:"token"`
 ServerTempLimit int `json:"serverTempLimit,omitempty"`
 SwitchTempLimit int `json:"switchTempLimit,omitempty"`
}
var telegramTokenPattern = regexp.MustCompile(`^[0-9]{1,20}:[A-Za-z0-9_-]{20,128}$`)
var telegramChannelPattern = regexp.MustCompile(`^@[A-Za-z0-9_]{5,64}$`)

func validateTelegram(in telegramInput, token string, required bool) error {
 if token != "" && !telegramTokenPattern.MatchString(token) { return errors.New("Неверный формат Bot Token") }
 if in.ChatID != "" {
  id, err := strconv.ParseInt(in.ChatID,10,64)
  if (err != nil || id == 0) && !telegramChannelPattern.MatchString(in.ChatID) { return errors.New("Укажите числовой Chat ID или @username канала") }
 }
 if required && (token == "" || in.ChatID == "") { return errors.New("Укажите Bot Token и Chat ID") }
 if in.TopicID < 0 || int64(in.TopicID) > 2147483647 { return errors.New("Некорректный ID темы") }
 if err := validateAlertSelection(MailConfig{Alerts:in.Alerts,ServerTempLimit:in.ServerTempLimit,SwitchTempLimit:in.SwitchTempLimit}); err != nil { return errors.New(strings.ReplaceAll(err.Error(),"SMTP","Telegram")) }
 return nil
}

func (s *server) getTelegram(w http.ResponseWriter, r *http.Request) {
 s.mu.Lock()
 cfg := s.state.Telegram
 cfg.HasToken = s.state.EncryptedTelegramToken != ""
 a,b := serverTempLimit(s.state.Mail),switchTempLimit(s.state.Mail)
 s.mu.Unlock()
 sendJSON(w,200,struct {
  TelegramConfig
  ServerTempLimit int `json:"serverTempLimit"`
  SwitchTempLimit int `json:"switchTempLimit"`
 }{cfg,a,b})
}

func (s *server) setTelegram(w http.ResponseWriter, r *http.Request) {
 var in telegramInput
 if decodeJSON(r,&in) != nil { problem(w,400,"Неверные настройки Telegram"); return }
 in.ChatID,in.Token = strings.TrimSpace(in.ChatID),strings.TrimSpace(in.Token)
 s.mu.Lock()
 defer s.mu.Unlock()
 old,oldEnc,oldMail := s.state.Telegram,s.state.EncryptedTelegramToken,s.state.Mail
 token := in.Token
 if token == "" {
  var err error
  token,err = s.decrypt(oldEnc)
  if err != nil { problem(w,500,"Не удалось прочитать сохранённый токен"); return }
 }
 if err := validateTelegram(in,token,in.Enabled); err != nil { problem(w,400,err.Error()); return }
 enc := oldEnc
 if in.Token != "" {
  var err error
  enc,err = s.encrypt(token)
  if err != nil { problem(w,500,"Не удалось сохранить токен"); return }
 }
 in.HasToken = false
 s.state.Telegram,s.state.EncryptedTelegramToken = in.TelegramConfig,enc
 if in.ServerTempLimit != 0 { s.state.Mail.ServerTempLimit = in.ServerTempLimit }
 if in.SwitchTempLimit != 0 { s.state.Mail.SwitchTempLimit = in.SwitchTempLimit }
 previous := s.state.Alerts
 if old.ChatID != in.ChatID || old.TopicID != in.TopicID || in.Token != "" || !in.Enabled {
  s.state.Alerts = slices.Clone(previous)
  for i := range s.state.Alerts {
   if s.state.Alerts[i].TelegramDelivery == "queued" { s.state.Alerts[i].TelegramDelivery = "disabled"; s.state.Alerts[i].TelegramDeliveryError = "" }
  }
 }
 if err := s.saveLocked(); err != nil {
  s.state.Telegram,s.state.EncryptedTelegramToken,s.state.Mail,s.state.Alerts = old,oldEnc,oldMail,previous
  problem(w,500,"Не удалось сохранить настройки Telegram"); return
 }
 sendJSON(w,200,map[string]string{"status":"ok"})
}

func (s *server) telegramToken(input string) (string,error) {
 if input = strings.TrimSpace(input); input != "" { return input,nil }
 s.mu.Lock()
 enc := s.state.EncryptedTelegramToken
 s.mu.Unlock()
 return s.decrypt(enc)
}
func (s *server) allowTelegramTest() bool {
 s.mu.Lock()
 defer s.mu.Unlock()
 if time.Since(s.telegramLast) < 3*time.Second { return false }
 s.telegramLast = time.Now()
 return true
}

// Never expose a request URL or remote error text: both may contain secrets.
var telegramHTTP = &http.Client{Timeout:12*time.Second,CheckRedirect:func(*http.Request,[]*http.Request) error {return http.ErrUseLastResponse}}
const telegramBase = "https://api.telegram.org"
type telegramResponse struct {
 OK bool `json:"ok"`
 ErrorCode int `json:"error_code"`
 Result json.RawMessage `json:"result"`
 Parameters struct { RetryAfter int `json:"retry_after"` } `json:"parameters"`
}
type telegramRateError struct { seconds int }
func (e telegramRateError) Error() string { return fmt.Sprintf("Telegram: превышен лимит запросов (повтор через %d с)",e.seconds) }

func telegramCall(ctx context.Context, client *http.Client, base,token,method string,payload any) (json.RawMessage,error) {
 if !telegramTokenPattern.MatchString(token) { return nil,errors.New("Неверный формат Bot Token") }
 b,err := json.Marshal(payload)
 if err != nil { return nil,errors.New("Не удалось подготовить запрос Telegram") }
 req,err := http.NewRequestWithContext(ctx,http.MethodPost,base+"/bot"+token+"/"+method,bytes.NewReader(b))
 if err != nil { return nil,errors.New("Не удалось подготовить запрос Telegram") }
 req.Header.Set("Content-Type","application/json")
 resp,err := client.Do(req)
 if err != nil { return nil,errors.New("Telegram недоступен или истекло время ожидания") }
 defer resp.Body.Close()
 var out telegramResponse
 if json.NewDecoder(io.LimitReader(resp.Body,4<<20)).Decode(&out) != nil { return nil,fmt.Errorf("Некорректный ответ Telegram (HTTP %d)",resp.StatusCode) }
 if resp.StatusCode >= 200 && resp.StatusCode < 300 && out.OK { return out.Result,nil }
 code := out.ErrorCode
 if code == 0 { code = resp.StatusCode }
 switch code {
 case 400: return nil,errors.New("Telegram: неверный Chat ID, тема или параметры сообщения")
 case 401,404: return nil,errors.New("Telegram: неверный или отозванный Bot Token")
 case 403: return nil,errors.New("Telegram: бот заблокирован или не имеет доступа к чату")
 case 409: return nil,errors.New("Telegram: конфликт getUpdates; проверьте webhook или другой экземпляр бота")
 case 429: return nil,telegramRateError{out.Parameters.RetryAfter}
 default: return nil,fmt.Errorf("Telegram: ошибка API (%d)",code)
 }
}
func telegramText(event Alert) string {
 icon := "ℹ️"
 switch event.Severity { case "critical": icon="🔴"; case "warning": icon="🟡" }
 for _,suffix := range []string{"_up","_cooled","_recovered","_finished","_ok"} { if strings.HasSuffix(event.Kind,suffix) {icon="🟢";break} }
 header := icon+" Storavix\n"+event.Subject+"\n"
 footer := "\nВремя события: "+event.CreatedAt.UTC().Format("02.01.2006 15:04:05")+" UTC"
 r := []rune(header+event.Detail)
 limit := 2000-len([]rune(footer))-1
 if len(r)>limit { r=append(r[:limit],'…') }
 return string(r)+footer
}
func sendTelegram(ctx context.Context,cfg TelegramConfig,token string,event Alert) error {
 payload := map[string]any{"chat_id":cfg.ChatID,"text":telegramText(event)}
 if cfg.TopicID>0 { payload["message_thread_id"]=cfg.TopicID }
 _,err := telegramCall(ctx,telegramHTTP,telegramBase,token,"sendMessage",payload)
 return err
}
func (s *server) testTelegram(w http.ResponseWriter,r *http.Request) {
 var in telegramInput
 if decodeJSON(r,&in)!=nil {problem(w,400,"Неверные настройки Telegram");return}
 in.ChatID=strings.TrimSpace(in.ChatID)
 token,err:=s.telegramToken(in.Token)
 if err!=nil {problem(w,500,"Не удалось прочитать токен");return}
 if err=validateTelegram(in,token,true);err!=nil {problem(w,400,err.Error());return}
 if !s.allowTelegramTest() {problem(w,429,"Повторите через 3 секунды");return}
 err=sendTelegram(r.Context(),in.TelegramConfig,token,Alert{CreatedAt:time.Now(),Severity:"info",Subject:"Тестовое оповещение",Detail:"Проверка доставки Telegram выполнена."})
 if err!=nil {problem(w,502,err.Error());return}
 sendJSON(w,200,map[string]string{"status":"sent"})
}
type telegramChat struct {
 ID int64 `json:"id"`
 Type string `json:"type"`
 Title string `json:"title"`
}
type telegramUpdate struct {
 Message *struct {Chat telegramChat `json:"chat"`} `json:"message"`
 ChannelPost *struct {Chat telegramChat `json:"chat"`} `json:"channel_post"`
}
func (s *server) telegramChats(w http.ResponseWriter,r *http.Request) {
 var in struct {Token string `json:"token"`}
 if decodeJSON(r,&in)!=nil {problem(w,400,"Неверный запрос");return}
 token,err:=s.telegramToken(in.Token)
 if err!=nil {problem(w,500,"Не удалось прочитать токен");return}
 if !telegramTokenPattern.MatchString(token) {problem(w,400,"Укажите корректный Bot Token");return}
 if !s.allowTelegramTest() {problem(w,429,"Повторите через 3 секунды");return}
 // Do not acknowledge updates or change any webhook.
 raw,err:=telegramCall(r.Context(),telegramHTTP,telegramBase,token,"getUpdates",map[string]any{"limit":100,"timeout":0})
 if err!=nil {problem(w,502,err.Error());return}
 var updates []telegramUpdate
 if json.Unmarshal(raw,&updates)!=nil {problem(w,502,"Не удалось прочитать список чатов");return}
 chats:=make([]map[string]string,0)
 seen:=map[int64]bool{}
 add:=func(c telegramChat) {
  if c.ID==0 || seen[c.ID] {return}
  seen[c.ID]=true
  chats=append(chats,map[string]string{"id":strconv.FormatInt(c.ID,10),"type":c.Type,"title":c.Title})
 }
 for _,u:=range updates {if u.Message!=nil {add(u.Message.Chat)};if u.ChannelPost!=nil {add(u.ChannelPost.Chat)}}
 sendJSON(w,200,chats)
}
func pendingTelegram(a Alert) bool {return a.TelegramDelivery=="queued" || a.TelegramDelivery=="sending"}
func (s *server) updateTelegramLocked(index int,status string,err error) bool {
 a:=&s.state.Alerts[index]
 previous:=*a
 a.TelegramDelivery,a.TelegramDeliveryError=status,""
 if err!=nil {a.TelegramDeliveryError=safeErr(err)}
 if err:=s.saveLocked();err!=nil {*a=previous;log.Printf("record telegram delivery: %v",err);return false}
 return true
}
func (s *server) deliverMonitorTelegram(ctx context.Context,send func(context.Context,TelegramConfig,string,Alert) error) (bool,time.Duration) {
 s.mu.Lock()
 index:=-1
 for i,a:=range s.state.Alerts {if a.TelegramDelivery=="queued" && (index<0 || a.CreatedAt.Before(s.state.Alerts[index].CreatedAt)) {index=i}}
 if index<0 {s.mu.Unlock();return false,0}
 event,cfg,enc:=s.state.Alerts[index],s.state.Telegram,s.state.EncryptedTelegramToken
 if !cfg.Enabled || !slices.Contains(cfg.Alerts,event.Kind) {ok:=s.updateTelegramLocked(index,"disabled",nil);s.mu.Unlock();return ok,0}
 if !s.updateTelegramLocked(index,"sending",nil) {s.mu.Unlock();return false,0}
 s.mu.Unlock()
 token,err:=s.decrypt(enc)
 if err!=nil {err=errors.New("Не удалось прочитать сохранённый токен")} else {err=send(ctx,cfg,token,event)}
 s.mu.Lock()
 defer s.mu.Unlock()
 for i,a:=range s.state.Alerts {
  if a.ID!=event.ID {continue}
  var rate telegramRateError
  if errors.As(err,&rate) {
   // Retry only a definite rejection, never an ambiguous network failure.
   if s.state.Telegram.Enabled && s.state.Telegram.ChatID==cfg.ChatID && s.state.Telegram.TopicID==cfg.TopicID && s.state.EncryptedTelegramToken==enc && slices.Contains(s.state.Telegram.Alerts,event.Kind) {
    seconds:=max(3,min(rate.seconds,3600))
    return s.updateTelegramLocked(i,"queued",err),time.Duration(seconds)*time.Second
   }
   return s.updateTelegramLocked(i,"disabled",nil),0
  }
  if err!=nil {return s.updateTelegramLocked(i,"failed",err),0}
  return s.updateTelegramLocked(i,"sent",nil),0
 }
 return false,0
}
func (s *server) runMonitorTelegram(ctx context.Context) {
 s.mu.Lock()
 for i,a:=range s.state.Alerts {if a.TelegramDelivery=="sending" {s.updateTelegramLocked(i,"unknown",errors.New("Служба остановилась до подтверждения отправки"))}}
 s.mu.Unlock()
 for ctx.Err()==nil {
  ok,delay:=s.deliverMonitorTelegram(ctx,sendTelegram)
  if delay==0 {delay=5*time.Second;if ok {delay=3*time.Second}}
  timer:=time.NewTimer(delay)
  select {case <-ctx.Done():timer.Stop();return;case <-timer.C:}
 }
}
