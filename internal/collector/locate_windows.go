package collector

import (
 "context"
 "errors"
 "time"
 "github.com/iPapik/Storavix/internal/telemetry"
)
func LocateDrive(c telemetry.LocateCommand) error {
 if _,_,err:=LocateTarget(c);err!=nil{return err}
 if !megaCommandMu.TryLock() {return errors.New("Идёт опрос RAID, повторите Locate через несколько секунд")}
 defer megaCommandMu.Unlock()
 // Use the same trusted CLI discovery as inventory. Never accept an executable path from the server.
 for _,path:=range windowsMegaTools() {
  if !isTool(path) || cliSource(path)!=c.Source {continue}
  ctx,cancel:=context.WithTimeout(context.Background(),35*time.Second)
  defer cancel()
  return locateWithRunner(c,func(args ...string)([]byte,error){return runStorageCLI(ctx,path,args...)})
 }
 return errors.New("Утилита StorCLI/PercCLI не найдена")
}
