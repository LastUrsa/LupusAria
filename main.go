package main

import (
	"context"
	"embed"
	"fmt"
	"net"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	mode := "desktop"
	if len(os.Args) > 1 && os.Args[1] == "--service" {
		mode = "service"
	}
	listener, err := net.Listen("tcp4", sipAddress)
	if err != nil {
		fmt.Fprintln(os.Stderr, "LupusAria could not claim its local service port. Check for an existing instance.")
		recordServiceStartup("LupusAria could not claim its local service port.")
		return
	}
	defer listener.Close()
	app := NewApp()
	if mode == "service" {
		runService(app, listener)
		return
	}

	server := app.sipServer(mode)
	err = wails.Run(&options.App{
		Title:     "LupusAria",
		Width:     1040,
		Height:    720,
		MinWidth:  940,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 35, G: 37, B: 56, A: 1},
		OnStartup: func(ctx context.Context) {
			app.startup(ctx)
			go server.Serve(listener)
		},
		OnShutdown: func(ctx context.Context) {
			_ = server.Close()
			app.shutdown(ctx)
		},
		Bind: []interface{}{
			app,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "starsong-tools-lupusaria",
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
