package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Schachte/pi-google-services/internal/auth"
	"github.com/Schachte/pi-google-services/internal/calendar"
	"github.com/Schachte/pi-google-services/internal/config"
	"github.com/Schachte/pi-google-services/internal/contacts"
	"github.com/Schachte/pi-google-services/internal/drive"
	"github.com/Schachte/pi-google-services/internal/gmail"
	"github.com/Schachte/pi-google-services/internal/mcp"
	"github.com/Schachte/pi-google-services/internal/services"
	"github.com/Schachte/pi-google-services/internal/tasks"
)

const version = "0.1.21"

func main() {
	log.SetFlags(0)
	log.SetPrefix("pi-google: ")

	if len(os.Args) < 2 {
		printUsage()
		return
	}

	switch os.Args[1] {
	case "login":
		cmdLogin(wantsNoBrowser(os.Args))
	case "logout":
		cmdLogout()
	case "setup":
		cmdSetup(wantsNoBrowser(os.Args))
	case "update":
		cmdUpdate()
	case "serve":
		cmdServe()
	case "status":
		cmdStatus()
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`pi-google-services v%s — Google services MCP server for Pi

Usage:
  pi-google-services setup          First-time setup (login + configure)
  pi-google-services login          Authenticate with Google
  pi-google-services logout         Remove stored credentials
  pi-google-services update         Self-update binary to latest version
  pi-google-services serve          Start MCP server (stdio)
  pi-google-services status         Show auth status
  pi-google-services help           Show this help

Install:  pi install npm:pi-google-services
Setup:    pi-google-services setup
Update:   pi-google-services update
`, version)
}

// getCredentialsJSON reads credentials from file or env var.
// Credentials are NOT embedded in the binary for security.
// install.js downloads both binary + credentials.json.
func getCredentialsJSON() ([]byte, error) {
	// 1. GOOGLE_OAUTH_CREDENTIALS env var
	if path := os.Getenv("GOOGLE_OAUTH_CREDENTIALS"); path != "" {
		return os.ReadFile(path)
	}
	// 2. Config directory (installed by install.js)
	if dir, err := config.Dir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(dir, "credentials.json")); err == nil {
			return data, nil
		}
	}
	// 3. Current directory (development)
	if data, err := os.ReadFile("credentials.json"); err == nil {
		return data, nil
	}
	return nil, fmt.Errorf("credentials.json not found. Run install.js or set GOOGLE_OAUTH_CREDENTIALS")
}

// registeredServices returns all available services with their scopes.
func registeredServices() []services.Service {
	return []services.Service{
		services.NewCalendar(nil), // placeholders; api set during serve
		services.NewGmail(nil, nil),
		services.NewTasks(nil),
		services.NewDrive(nil),
		services.NewContacts(nil),
	}
}

// allScopes aggregates scopes from all registered services.
func allScopes() []string {
	seen := map[string]bool{}
	var scopes []string
	for _, svc := range registeredServices() {
		for _, s := range svc.Scopes() {
			if !seen[s] {
				seen[s] = true
				scopes = append(scopes, s)
			}
		}
	}
	return scopes
}

// newAuthenticator builds an OAuth authenticator from the client credentials,
// always requesting every registered service scope. login, setup and serve all
// share this single source of truth, so every flow asks Google for the same
// permissions — no hardcoded subsets, no drift between commands.
func newAuthenticator(credsData []byte) (*auth.Authenticator, error) {
	creds, err := config.LoadCredentialsFromBytes(credsData)
	if err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	return auth.NewFromCredentials(creds, allScopes()), nil
}

// All tools aggregated from all services.
func allTools() []mcp.ToolDefinition {
	var tools []mcp.ToolDefinition
	for _, svc := range registeredServices() {
		tools = append(tools, svc.Tools()...)
	}
	return tools
}

// wantsNoBrowser reports whether --no-browser was passed anywhere in args,
// forcing the manual paste-code OAuth flow (headless hosts, WSL, SSH).
func wantsNoBrowser(args []string) bool {
	for _, a := range args {
		if a == "--no-browser" {
			return true
		}
	}
	return false
}

func cmdLogin(noBrowser bool) {
	credsData, err := getCredentialsJSON()
	if err != nil {
		fmt.Println("❌ Credentials not found.")
		fmt.Println("   Set GOOGLE_OAUTH_CREDENTIALS or copy credentials.json into place")
		os.Exit(1)
	}

	a, err := newAuthenticator(credsData)
	if err != nil {
		log.Fatalf("Invalid credentials: %v", err)
	}

	ctx := context.Background()

	fmt.Println("\n🔐 Authorizing with Google...")
	if noBrowser {
		fmt.Println("   Manual mode (--no-browser): paste the authorization URL.")
	}
	fmt.Printf("   Scopes requested: %d services\n", len(registeredServices()))
	_, err = a.LoginWithOptions(ctx, auth.LoginOptions{NoBrowser: noBrowser})
	if err != nil {
		log.Fatalf("Login failed: %v", err)
	}

	fmt.Printf("\n✅ Login successful!\n")
	fmt.Printf("\nNow run:  %s serve\n", os.Args[0])
}

func cmdLogout() {
	dir, err := config.Dir()
	if err != nil {
		log.Fatalf("Config dir: %v", err)
	}
	tokenPath := filepath.Join(dir, config.TokenFile)
	if err := os.Remove(tokenPath); os.IsNotExist(err) {
		fmt.Println("No credentials stored.")
		return
	} else if err != nil {
		log.Fatalf("Error removing: %v", err)
	}
	fmt.Println("✅ Credentials removed.")
}

func cmdServe() {
	token, err := auth.LoadToken()
	if err != nil {
		log.Fatalf("Token: %v", err)
	}
	if token == nil {
		log.Fatal("❌ Not authenticated. Run 'pi-google-services login' first.")
	}

	// Always build the authenticator from client credentials with all scopes.
	// Never trust scopes persisted in config.json — they can drift out of sync
	// with the registered services (see issue #6).
	credsData, err := getCredentialsJSON()
	if err != nil {
		log.Fatalf("credentials.json not found: %v", err)
	}
	a, err := newAuthenticator(credsData)
	if err != nil {
		log.Fatalf("Invalid credentials: %v", err)
	}

	ctx := context.Background()
	ts := a.TokenSource(ctx, token)

	// Create services
	calSvc, err := calendar.New(ctx, ts)
	if err != nil {
		log.Fatalf("Calendar: %v", err)
	}

	gmailSvc, err := gmail.New(ctx, ts)
	if err != nil {
		log.Fatalf("Gmail: %v", err)
	}

	tasksSvc, err := tasks.New(ctx, ts)
	if err != nil {
		log.Fatalf("Tasks: %v", err)
	}

	driveSvc, err := drive.New(ctx, ts)
	if err != nil {
		log.Fatalf("Drive: %v", err)
	}

	contactsSvc, err := contacts.New(ctx, ts)
	if err != nil {
		log.Fatalf("Contacts: %v", err)
	}

	// Build MCP server with registered services
	server := mcp.New()
	registerServiceTools(server, services.NewCalendar(calSvc))
	registerServiceTools(server, services.NewGmail(gmailSvc, driveSvc))
	registerServiceTools(server, services.NewTasks(tasksSvc))
	registerServiceTools(server, services.NewDrive(driveSvc))
	registerServiceTools(server, services.NewContacts(contactsSvc))

	log.Println("✅ Google Services MCP server started (stdio)")
	log.Printf("   Tools registered: %d\n", len(server.Tools()))

	if err := server.Run(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func cmdSetup(noBrowser bool) {
	// Login flow
	credsData, err := getCredentialsJSON()
	if err != nil {
		fmt.Println("❌ credentials.json not found.")
		fmt.Println("   Make sure you installed the package with: pi install npm:pi-google-services")
		os.Exit(1)
	}

	// Check if already authenticated
	token, err := auth.LoadToken()
	if err == nil && token != nil {
		fmt.Println("✅ Already authenticated.")
		fmt.Println()
		fmt.Println("  To connect to Pi:")
		fmt.Println("    1. Restart the Pi session")
		fmt.Println("    2. Try: 'show my emails' or 'list my events'")
		return
	}

	a, err := newAuthenticator(credsData)
	if err != nil {
		log.Fatalf("Invalid credentials: %v", err)
	}
	ctx := context.Background()

	fmt.Println("\n🔐 Authorizing with Google...")
	if noBrowser {
		fmt.Println("   Manual mode (--no-browser): paste the authorization URL.")
	}
	token, err = a.LoginWithOptions(ctx, auth.LoginOptions{NoBrowser: noBrowser})
	if err != nil {
		log.Fatalf("Login failed: %v", err)
	}

	fmt.Printf("\n✅ Setup complete!\n")
	fmt.Println()
	fmt.Println("  To start using it:")
	fmt.Println("    1. Restart the Pi session")
	fmt.Println("    2. Try: 'show my emails' or 'create a meet tomorrow at 10 AM'")
}

func cmdUpdate() {
	fmt.Println("\n🔍 Checking for updates...")

	latest, err := fetchLatestVersion()
	if err != nil {
		log.Fatalf("Error checking for version: %v", err)
	}

	current := strings.TrimPrefix(version, "v")
	latest = strings.TrimPrefix(latest, "v")

	switch {
	case latest == "" || latest == current:
		fmt.Printf("✅ You already have the latest version (%s)\n", version)
		return
	case compareVersions(latest, current) > 0:
		fmt.Printf("📦 New version available: %s (current: %s)\n", latest, version)
		if err := downloadUpdate(latest); err != nil {
			log.Fatalf("Update failed: %v", err)
		}
		fmt.Printf("\n✅ Updated to v%s\n", latest)
		fmt.Println("   Restart the Pi session to use the new version.")
	default:
		fmt.Printf("✅ You already have the latest version (%s)\n", version)
	}
}

func fetchLatestVersion() (string, error) {
	// GitHub API: get latest release tag
	resp, err := http.Get("https://api.github.com/repos/Schachte/pi-google-services/releases/latest")
	if err != nil {
		return "", fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var result struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	return result.TagName, nil
}

func downloadUpdate(version string) error {
	plat := runtime.GOOS + "-" + runtime.GOARCH
	switch plat {
	case "linux-amd64", "linux-arm64", "darwin-arm64":
		// supported
	default:
		return fmt.Errorf("unsupported platform: %s", plat)
	}

	url := fmt.Sprintf("https://github.com/Schachte/pi-google-services/releases/download/v%s/pi-google-services-%s.gz", version, plat)
	fmt.Printf("   ⬇ Downloading %s...\n", url)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Read gzip data
	gzipData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	// Get current binary path
	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("self path: %w", err)
	}

	// Decompress and write
	// We need compress/gzip
	// Write to temp file first, then rename
	tmpPath := selfPath + ".new"
	if err := decompressGzip(gzipData, tmpPath, 0755); err != nil {
		return fmt.Errorf("decompress: %w", err)
	}

	if err := os.Rename(tmpPath, selfPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace: %w", err)
	}

	return nil
}

func decompressGzip(data []byte, path string, mode os.FileMode) error {
	// Decompress gzip data and write to file
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, gr); err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	return nil
}

func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		var ai, bi int
		fmt.Sscanf(aParts[i], "%d", &ai)
		fmt.Sscanf(bParts[i], "%d", &bi)
		if ai > bi {
			return 1
		}
		if ai < bi {
			return -1
		}
	}
	return 0
}

func registerServiceTools(server *mcp.Server, svc services.Service) {
	for _, tool := range svc.Tools() {
		t := tool // capture
		name := t.Name
		server.RegisterTool(name, t, func(ctx context.Context, params json.RawMessage) (interface{}, *mcp.RPCError) {
			return svc.Handle(ctx, name, params)
		})
	}
}

func cmdStatus() {
	token, err := auth.LoadToken()
	if err != nil {
		fmt.Println("⚠ Error reading token:", err)
		return
	}
	if token == nil {
		fmt.Println("❌ Not authenticated.")
		fmt.Println("   Run: pi-google-services login")
		return
	}
	fmt.Println("✅ Authenticated")
	if !token.Expiry.IsZero() {
		fmt.Printf("  Token expires: %s\n", token.Expiry.Format("2006-01-02 15:04 MST"))
		if token.Expiry.Before(time.Now()) {
			fmt.Println("  ⚠ Token expired, it will be refreshed when serve starts")
		}
	}
	fmt.Printf("\n  Available services:\n")
	for _, svc := range registeredServices() {
		fmt.Printf("    • %s (%d tools)\n", svc.Name(), len(svc.Tools()))
	}
	fmt.Printf("\n  To start: pi-google-services serve\n")
}
