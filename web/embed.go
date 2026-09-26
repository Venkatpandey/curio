package web

import "embed"

//go:embed templates/*.html static/* static/photos/*
var Files embed.FS
