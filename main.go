package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"

	"kt-proxy/internal/configmgr"
	"kt-proxy/internal/daedsync"
	"kt-proxy/internal/server"
)

//go:embed web/static/*
var embeddedStatic embed.FS

func main() {
	addr := env("KT_PROXY_ADDR", ":8090")
	manager := configmgr.New(configmgr.Paths{
		ConfigPath:  env("SING_BOX_CONFIG_PATH", "/etc/sing-box/config.json"),
		ExamplePath: env("SING_BOX_EXAMPLE_PATH", "sing-box-config-example.json"),
	}, configmgr.Commands{
		SingBox:   env("SING_BOX_BIN", "sing-box"),
		Systemctl: env("SYSTEMCTL_BIN", "systemctl"),
	}, nil)

	staticFS, err := fs.Sub(embeddedStatic, "web/static")
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}
	daed := daedsync.New(daedsync.Config{
		GraphQLURL:    os.Getenv("DAED_GRAPHQL_URL"),
		Authorization: os.Getenv("DAED_AUTHORIZATION"),
	}, manager, nil)
	handler := server.New(manager, staticFS, daed)
	log.Printf("kt-proxy listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func env(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
