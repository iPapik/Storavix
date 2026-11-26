package collector

import (
 "errors"
 "regexp"
 "strings"
 "sync"
 "github.com/iPapik/Storavix/internal/telemetry"
)
var megaCommandMu sync.Mutex
var locateController = regexp.MustCompile(`^mega:(/c[0-9]{1,5})$`)
var locateSlot = regexp.MustCompile(`^([0-9]{0,5}):([0-9]{1,5})$`)

// LocateTarget refuses ambiguous IDs, wildcard paths, unknown sources and missing serials.
func LocateTarget(c telemetry.LocateCommand) (string,string,error) {
 m,s := locateController.FindStringSubmatch(c.Controller),locateSlot.FindStringSubmatch(c.Slot)
 if m==nil || s==nil || strings.TrimSpace(c.Serial)=="" || len(c.Serial)>160 || (c.Action!="start" && c.Action!="stop") {
  return "","",errors.New("Невозможно однозначно определить диск")
 }
 switch c.Source {case "storcli","perccli","storcli2","perccli2": default:return "","",errors.New("Контроллер не поддерживает Locate")}
 path:=m[1]
 if s[1]!="" {path+="/e"+s[1]}
 path+="/s"+s[2]
 return m[1],path,nil
}
func locateWithRunner(c telemetry.LocateCommand, run func(...string)([]byte,error)) error {
 controller,path,err:=LocateTarget(c)
 if err!=nil{return err}
 if c.ControllerSerial!="" {
  raw,e:=run(controller,"show","all","J")
  rows,status:=decodeMega(raw)
  if e!=nil || status!="ok" || len(rows)!=1 || "/c"+rows[0].Command.value("Controller")!=controller || clean(rows[0].Data.object("Basics").value("Serial Number"))!=c.ControllerSerial {
   return errors.New("Серийный номер контроллера не подтверждён")
  }
 }
 raw,e:=run(path,"show","all","J")
 rows,status:=decodeMega(raw)
 if e!=nil || status!="ok" || len(rows)!=1 || "/c"+rows[0].Command.value("Controller")!=controller {
  return errors.New("Не удалось перечитать выбранный диск")
 }
 details:=megaDetails(rows)
 info,ok:=details[megaKey{controller,c.Slot}]
 if !ok || len(details)!=1 || clean(info.value("SN","Serial Number"))!=c.Serial {
  return errors.New("Серийный номер диска изменился или не подтверждён; Locate отменён")
 }
 raw,e=run(path,c.Action,"locate","J")
 rows,status=decodeMega(raw)
 if e!=nil || status!="ok" || len(rows)!=1 || "/c"+rows[0].Command.value("Controller")!=controller {
  return errors.New("Контроллер не подтвердил Locate; проверьте LED физически")
 }
 return nil
}
