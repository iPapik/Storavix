package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var static embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
