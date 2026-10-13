package main

import (
    "encoding/json"
    "errors"
    "flag"
    "fmt"
    "io"
    "log"
    "net"
    "os"
    "path/filepath"
    "sort"
    "strconv"
    "strings"
)

type InstanceConfig struct { Listen string `json:"listen"` }

func validateListen(addr string) error {
    host, port, err := net.SplitHostPort(addr)
    if err != nil { return fmt.Errorf("некорректный адрес прослушивания %q: %w", addr, err) }
    p, err := strconv.Atoi(port)
    if err != nil || p < 1 || p > 65535 { return errors.New("порт должен быть от 1 до 65535") }
    if host != "" && host != "localhost" && net.ParseIP(host) == nil { return errors.New("используйте 0.0.0.0, 127.0.0.1 или IP-адрес интерфейса") }
    return nil
}
func readInstanceConfig(dir string) (InstanceConfig, error) {
    var cfg InstanceConfig
    b, err := os.ReadFile(filepath.Join(dir, "instance.json"))
    if errors.Is(err, os.ErrNotExist) { return cfg, nil }
    if err != nil { return cfg, err }
    if err := json.Unmarshal(b, &cfg); err != nil { return cfg, fmt.Errorf("ошибка instance.json: %w",err) }
    if err := validateListen(cfg.Listen); err != nil { return cfg, err }
    return cfg, nil
}
func configuredListen(dir, explicit string) (string,error) {
    if explicit!="" {return explicit, validateListen(explicit)}
    if x:=os.Getenv("STORAVIX_LISTEN");x!="" {return x,validateListen(x)}
    cfg,err:=readInstanceConfig(dir)
    if err!=nil {return "",err}
    if cfg.Listen!="" {return cfg.Listen,nil}
    return "127.0.0.1:8080",nil
}
func accessURLs(listen string) []string {
    host,port,err:=net.SplitHostPort(listen)
    if err!=nil {return nil}
    urls:=map[string]bool{}
    add:=func(ip string){if ip!="" {urls["http://"+net.JoinHostPort(ip,port)]=true}}
    if host=="0.0.0.0" || host=="" {
        add("127.0.0.1")
        ifaces,e:=net.Interfaces()
        if e==nil {
            for _,nic:=range ifaces {
                if nic.Flags&net.FlagUp==0 || nic.Flags&net.FlagLoopback!=0 {continue}
                addrs,_:=nic.Addrs()
                for _,a:=range addrs {
                    var ip net.IP
                    switch x:=a.(type){case *net.IPNet:ip=x.IP;case *net.IPAddr:ip=x.IP}
                    if ip4:=ip.To4();ip4!=nil && !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() && !ip4.IsMulticast() {add(ip4.String())}
                }
            }
        }
    } else if host=="localhost" {add("127.0.0.1")} else {add(host)}
    out:=make([]string,0,len(urls))
    for u:=range urls {out=append(out,u)}
    sort.Strings(out)
    return out
}
func executeCLI(args []string, stdin io.Reader, output io.Writer) (bool,string,string,error) {
    if len(args)>0 && args[0]=="configure" {
        flags:=flag.NewFlagSet("configure",flag.ContinueOnError);flags.SetOutput(output)
        dir:=flags.String("data-dir",env("STORAVIX_DATA_DIR","./data"),"existing installation directory")
        listen:=flags.String("listen","0.0.0.0:8080","listen address")
        if err:=flags.Parse(args[1:]);err!=nil{return true,"","",err}
        if flags.NArg()!=0{return true,"","",errors.New("неизвестные параметры configure")}
        if err:=validateListen(*listen);err!=nil{return true,"","",err}
        if _,err:=os.Stat(filepath.Join(*dir,"state.json"));err!=nil{return true,"","",errors.New("сначала выполните init: state.json отсутствует")}
        cfg,_:=json.MarshalIndent(InstanceConfig{Listen:*listen},"","  ")
        if err:=os.WriteFile(filepath.Join(*dir,"instance.json"),cfg,0600);err!=nil{return true,"","",err}
        fmt.Fprintln(output,"Адрес прослушивания обновлён. Перезапустите Storavix.")
        for _,u:=range accessURLs(*listen){fmt.Fprintln(output,"Адрес для браузера:",u)}
        return true,"","",nil
    }
    if len(args)>0 && args[0]=="init" {
        flags:=flag.NewFlagSet("init",flag.ContinueOnError);flags.SetOutput(output)
        dir:=flags.String("data-dir",env("STORAVIX_DATA_DIR","./data"),"installation directory")
        listen:=flags.String("listen","0.0.0.0:8080","listen address")
        user:=flags.String("admin-user","admin","initial admin login")
        passwordStdin:=flags.Bool("password-stdin",false,"read password from stdin")
        if err:=flags.Parse(args[1:]);err!=nil{return true,"","",err}
        if flags.NArg()!=0{return true,"","",errors.New("неизвестные параметры init")}
        if err:=validateListen(*listen);err!=nil{return true,"","",err}
        if len(*user)<3||len(*user)>64||strings.Trim(*user,"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-")!=""{return true,"","",errors.New("логин: 3–64 символа, латинские буквы, цифры, точка, дефис или _")}
        if !*passwordStdin{return true,"","",errors.New("укажите --password-stdin для безопасной передачи пароля")}
        b,err:=io.ReadAll(io.LimitReader(stdin,2049))
        if err!=nil{return true,"","",err}
        pass:=strings.TrimRight(string(b),"\r\n")
        if len(pass)<12||len(pass)>1024||strings.ContainsAny(pass,"\x00\r\n"){return true,"","",errors.New("пароль должен быть длиной 12–1024 символа без переносов строк")}
        for _,f:=range []string{"state.json","instance.json"} {
            if _,e:=os.Stat(filepath.Join(*dir,f));e==nil{return true,"","",errors.New("установка уже инициализирована — настройки и учётная запись не изменены")}else if !errors.Is(e,os.ErrNotExist){return true,"","",e}
        }
        os.Setenv("STORAVIX_ADMIN_USER",*user)
        os.Setenv("STORAVIX_ADMIN_PASSWORD",pass)
        defer os.Unsetenv("STORAVIX_ADMIN_USER")
        defer os.Unsetenv("STORAVIX_ADMIN_PASSWORD")
        if _,err:=newServer(*dir);err!=nil{return true,"","",err}
        cfg,_:=json.MarshalIndent(InstanceConfig{Listen:*listen},"","  ")
        if err:=os.WriteFile(filepath.Join(*dir,"instance.json"),cfg,0600);err!=nil{return true,"","",err}
        fmt.Fprintf(output,"Storavix: создан администратор %q. Настройки сохранены в %s\n",*user,*dir)
        for _,u:=range accessURLs(*listen){fmt.Fprintln(output,"Адрес для браузера:",u)}
        return true,"","",nil
    }
    flags:=flag.NewFlagSet("storavix",flag.ContinueOnError);flags.SetOutput(output)
    dir:=flags.String("data-dir",env("STORAVIX_DATA_DIR","./data"),"data directory")
    listen:=flags.String("listen","","listen override")
    logfile:=flags.String("log-file","","append logs to file")
    if err:=flags.Parse(args);err!=nil{return true,"","",err}
    if flags.NArg()!=0{return true,"","",errors.New("неизвестная команда: используйте storavix init")}
    addr,err:=configuredListen(*dir,*listen)
    if err!=nil{return true,"","",err}
    return false,*dir,addr,setupLogFile(*logfile)
}
func setupLogFile(path string) error {
    if path==""{return nil}
    if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{return err}
    f,err:=os.OpenFile(path,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600)
    if err!=nil{return err}
    log.SetOutput(io.MultiWriter(os.Stdout,f))
    return nil
}
