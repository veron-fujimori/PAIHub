package main

import (
	"bufio"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"paihub/internal/agents"
	"paihub/internal/config"
	"paihub/internal/server"
	"paihub/internal/sysenv"
)

//go:embed all:public
var embeddedPublic embed.FS

func main() {
	portFlag := flag.Int("port", 0, "override server port")
	dirFlag := flag.String("dir", "", "override workspace application root directory")
	bindFlag := flag.String("bind", "0.0.0.0", "bind host address")
	flag.Parse()

	sysenv.EnrichPath()
	selfDir := resolveSelfDir(*dirFlag)
	cleanupStagedBinary(selfDir)

	dataDir := filepath.Join(selfDir, "data")
	_ = os.MkdirAll(filepath.Join(dataDir, "plugins"), 0755)

	cfgStore, err := config.LoadStore(filepath.Join(dataDir, "config.json"), selfDir)
	if err != nil {
		log.Fatalf("Failed to load data/config.json: %v", err)
	}
	if *portFlag > 0 && *portFlag <= 65535 {
		_ = cfgStore.Update(func(c *config.Config) {
			c.Port = *portFlag
		})
	}

	agentMgr, err := agents.NewManager(filepath.Join(dataDir, "agents.json"))
	if err != nil {
		log.Fatalf("Failed to load data/agents.json: %v", err)
	}

	publicFS, err := fs.Sub(embeddedPublic, "public")
	if err != nil {
		log.Fatalf("Failed to load embedded public assets: %v", err)
	}

	srvApp, err := server.New(selfDir, cfgStore, agentMgr, publicFS)
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	cfg := cfgStore.Get()
	addr := net.JoinHostPort(strings.TrimSpace(*bindFlag), strconv.Itoa(cfg.Port))

	fmt.Println("==================================================")
	fmt.Printf("  PAIHub v%s\n", server.AppVersion)
	fmt.Println("==================================================")
	fmt.Printf("  Local   : http://localhost:%d\n", cfg.Port)
	for _, ip := range sysenv.LocalIPs() {
		fmt.Printf("  Network : http://%s:%d\n", ip, cfg.Port)
	}
	fmt.Printf("  Folder  : %s\n", selfDir)
	fmt.Println("--------------------------------------------------")
	fmt.Println("  Press [Enter] or [Ctrl+C] to exit...")
	fmt.Println("==================================================")

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srvApp.Routes(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		reader := bufio.NewReader(os.Stdin)
		if _, err := reader.ReadString('\n'); err == nil {
			stop()
		}
	}()

	go func() {
		<-ctx.Done()
		fmt.Println("\nShutting down PAIHub gracefully...")
		srvApp.Stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server error: %v", err)
	}
}

func cleanupStagedBinary(selfDir string) {
	name := "paihub.new"
	if runtime.GOOS == "windows" {
		name = "paihub.new.exe"
	}
	_ = os.Remove(filepath.Join(selfDir, name))
}

func resolveSelfDir(override string) string {
	if strings.TrimSpace(override) != "" {
		if abs, err := filepath.Abs(override); err == nil {
			return filepath.Clean(abs)
		}
		return filepath.Clean(override)
	}
	if cwd, err := os.Getwd(); err == nil {
		if _, statErr := os.Stat(filepath.Join(cwd, "public", "index.html")); statErr == nil {
			return filepath.Clean(cwd)
		}
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Clean(filepath.Dir(exe))
		if _, statErr := os.Stat(filepath.Join(exeDir, "public", "index.html")); statErr == nil {
			return exeDir
		}
		tempDir := filepath.Clean(os.TempDir())
		if tempDir == "" || !strings.HasPrefix(strings.ToLower(exeDir), strings.ToLower(tempDir)) {
			return exeDir
		}
	}
	cwd, _ := os.Getwd()
	return filepath.Clean(cwd)
}
