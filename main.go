package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"qoder2api/server"

	"github.com/joho/godotenv"
)

func loadEnv() {
	candidates := []string{".env"}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), ".env"))
	}
	candidates = append(candidates, "../.env")

	for _, path := range candidates {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := godotenv.Load(path); err != nil {
			log.Printf("Failed to load %s: %v", path, err)
			continue
		}
		log.Printf("Loaded env from %s", path)
		return
	}
	log.Println("No .env file found, using environment variables")
}

func printBanner() {
	cyan := "\033[36m"
	yellow := "\033[33m"
	magenta := "\033[35m"
	green := "\033[32m"
	reset := "\033[0m"

	fmt.Println()
	fmt.Println(cyan + "  ██████╗   ██████╗  ██████╗  ███████╗ ██████╗  ██████╗   █████╗  ██████╗  ██╗" + reset)
	fmt.Println(cyan + " ██╔═══██╗ ██╔═══██╗ ██╔══██╗ ██╔════╝ ██╔══██╗ ╚════██╗ ██╔══██╗ ██╔══██╗ ██║" + reset)
	fmt.Println(cyan + " ██║   ██║ ██║   ██║ ██║  ██║ █████╗   ██████╔╝  █████╔╝ ███████║ ██████╔╝ ██║" + reset)
	fmt.Println(cyan + " ██║▄▄ ██║ ██║   ██║ ██║  ██║ ██╔══╝   ██╔══██╗ ██╔═══╝  ██╔══██║ ██╔═══╝  ██║" + reset)
	fmt.Println(cyan + " ╚██████╔╝ ╚██████╔╝ ██████╔╝ ███████╗ ██║  ██║ ███████╗ ██║  ██║ ██║      ██║" + reset)
	fmt.Println(cyan + "  ╚══▀▀═╝   ╚═════╝  ╚═════╝  ╚══════╝ ╚═╝  ╚═╝ ╚══════╝ ╚═╝  ╚═╝ ╚═╝      ╚═╝" + reset)
	fmt.Println()
	fmt.Println(yellow + "  📧 Telegram:" + reset + " https://t.me/D3_vin")
	fmt.Println(magenta + "  👤 Author:" + reset + " @D3vin_dev")
	fmt.Println(green + "  🔗 GitHub:" + reset + " https://github.com/D3-vin/Qoder2Api")
	fmt.Println(cyan + "  📦 Qoder CLI:" + reset + " 1.0.0")
	fmt.Println()
}

func main() {
	loadEnv()
	printBanner()

	cfg := server.LoadConfig()
	if len(cfg.Pats) == 0 {
		cfg.Pats = server.PatsFromEnv()
	}
	if len(cfg.Pats) == 0 {
		fmt.Println("Error: no PAT configured. Set QODER_PAT or QODER_PAT_LIST in .env")
		os.Exit(1)
	}

	portStr := os.Getenv("QODER_PORT")
	port := 8963
	if portStr != "" {
		var err error
		port, err = strconv.Atoi(portStr)
		if err != nil {
			fmt.Printf("Invalid QODER_PORT: %s, using default 8963\n", portStr)
			port = 8963
		}
	}

	pool, err := server.NewBridgePool(cfg)
	if err != nil {
		fmt.Printf("Failed to create bridge pool: %v\n", err)
		os.Exit(1)
	}

	if err := pool.Start(port); err != nil {
		fmt.Printf("Server error: %v\n", err)
		os.Exit(1)
	}
}
