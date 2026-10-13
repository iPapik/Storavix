package main
import ("bytes";"os";"path/filepath";"strings";"testing")
func TestInstallInitAndPreserve(t *testing.T) {
 dir:=t.TempDir();var out bytes.Buffer
 ok,_,_,e:=executeCLI([]string{"init","--data-dir",dir,"--admin-user","operator","--listen","0.0.0.0:8080","--password-stdin"},strings.NewReader("long-password-for-tests\n"),&out)
 if e!=nil||!ok{t.Fatal(e)}
 for _,f:=range []string{"state.json","instance.json","master.key"}{if _,e=os.Stat(filepath.Join(dir,f));e!=nil{t.Fatal(e)}}
 s,e:=newServer(dir);if e!=nil||s.state.AdminUser!="operator"{t.Fatal(e)}
 if _,_,_,e=executeCLI([]string{"init","--data-dir",dir,"--password-stdin"},strings.NewReader("long-password-again\n"),&out);e==nil{t.Fatal("reinitialization must fail")}
 if _,_,_,e=executeCLI([]string{"configure","--data-dir",dir,"--listen","127.0.0.1:9090"},strings.NewReader(""),&out);e!=nil{t.Fatal(e)}
 addr,e:=configuredListen(dir,"");if e!=nil||addr!="127.0.0.1:9090"{t.Fatal(addr,e)}
}