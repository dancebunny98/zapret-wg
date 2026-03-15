package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vpngw/internal/app"
	"vpngw/internal/bootstrap"
	"vpngw/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "server":
		runServer(os.Args[2:])
	case "gen-client":
		runGenClient(os.Args[2:])
	case "bootstrap":
		runBootstrap(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	cfgPath := fs.String("config", "/etc/vpngw/config.json", "Path to config JSON")
	fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	srv, err := app.NewServer(cfg)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		log.Fatalf("start: %v", err)
	}
}

func runGenClient(args []string) {
	fs := flag.NewFlagSet("gen-client", flag.ExitOnError)
	cfgPath := fs.String("config", "/etc/vpngw/config.json", "Path to config JSON")
	name := fs.String("name", "", "Client name")
	fs.Parse(args)
	if *name == "" {
		log.Fatal("client name is required")
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	srv, err := app.NewServer(cfg)
	if err != nil {
		log.Fatalf("create app: %v", err)
	}

	client, cfgText, err := srv.CreateClient(context.Background(), *name)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	fmt.Printf("client_id=%s\nclient_ip=%s\n\n%s\n", client.ID, client.IPv4, cfgText)
}

func usage() {
	fmt.Println(`vpngw:
  vpngw server -config /etc/vpngw/config.json
  vpngw gen-client -config /etc/vpngw/config.json -name home-router
  vpngw bootstrap -clients 3 -allow-net-install`)
}

func runBootstrap(args []string) {
	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	configPath := fs.String("config", "/root/proga/config.json", "Config path")
	installDir := fs.String("install-dir", "/root/proga", "Install dir")
	servicePath := fs.String("service", "/etc/systemd/system/vpngw.service", "Systemd unit path")
	wgDir := fs.String("wg-dir", "/etc/wireguard", "WireGuard config dir")
	serverEP := fs.String("server-endpoint", "", "Public endpoint for clients (ip_or_dns:port)")
	clients := fs.Int("clients", 3, "How many clients create after install")
	force := fs.Bool("force", false, "Overwrite existing config files")
	allowNetInstall := fs.Bool("allow-net-install", false, "Allow bootstrap to use package managers if dependencies are missing")
	fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := bootstrap.Run(ctx, bootstrap.Options{
		ConfigPath:      *configPath,
		InstallDir:      *installDir,
		ServicePath:     *servicePath,
		WGDir:           *wgDir,
		ServerEP:        *serverEP,
		CreateClient:    *clients,
		Force:           *force,
		AllowNetInstall: *allowNetInstall,
	}); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}
	fmt.Println("bootstrap completed")
}
