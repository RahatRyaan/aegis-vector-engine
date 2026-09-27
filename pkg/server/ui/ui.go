package ui

import (
	"embed"
	"net/http"
)

//go:embed index.html
var Assets embed.FS

func Handler() http.Handler {
	return http.FileServer(http.FS(Assets))
}
