package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
)

const apiKey = "YOUR_STEAM_API_KEY"

func getFreePort() (string, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	addr := l.Addr().String()
	parts := strings.Split(addr, ":")
	return parts[len(parts)-1], nil
}

func openBrowser(uri string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "linux":
		cmd = "xdg-open"
		args = []string{uri}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", uri}
	case "darwin":
		cmd = "open"
		args = []string{uri}
	default:
		return fmt.Errorf("unsupported platform")
	}
	return exec.Command(cmd, args...).Start()
}

func main() {
	port, err := getFreePort()
	if err != nil {
		log.Fatalf("could not get free port: %v", err)
	}

	redirectURL := fmt.Sprintf("http://localhost:%s/callback", port)
	realmURL := fmt.Sprintf("http://localhost:%s", port)

	loginURL := fmt.Sprintf("https://steamcommunity.com/openid/login"+
		"?openid.ns=%s"+
		"&openid.mode=checkid_setup"+
		"&openid.return_to=%s"+
		"&openid.realm=%s"+
		"&openid.identity=%s"+
		"&openid.claimed_id=%s",
		url.QueryEscape("http://specs.openid.net/auth/2.0"),
		url.QueryEscape(redirectURL),
		url.QueryEscape(realmURL),
		url.QueryEscape("http://specs.openid.net/auth/2.0/identifier_select"),
		url.QueryEscape("http://specs.openid.net/auth/2.0/identifier_select"),
	)

	fmt.Println("Opening Steam login in your browser.")
	if err := openBrowser(loginURL); err != nil {
		fmt.Println("Cannot open browser. Please visit this URL manually:")
		fmt.Println(loginURL)
	}

	authChan := make(chan string)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		claimedID := r.URL.Query().Get("openid.claimed_id")
		if claimedID == "" {
			http.Error(w, "Missing claimed_id", http.StatusBadRequest)
			return
		}
		parts := strings.Split(claimedID, "/")
		steamID64 := parts[len(parts)-1]
		fmt.Fprintln(w, "Authentication complete. You may close this window.")
		authChan <- steamID64
	})

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	steamID64 := <-authChan

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)

	listGames(steamID64)
}

func listGames(steamID64 string) {	
	url := fmt.Sprintf(
		"https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?key=%s&steamid=%s&include_appinfo=1&include_played_free_games=1",
		apiKey, steamID64,
	)
	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Failed to fetch games: %v", err)
	}
	defer resp.Body.Close()

	type Game struct {
		Name            string `json:"name"`
		PlaytimeForever int    `json:"playtime_forever"`
	}
	type Response struct {
		GameCount int    `json:"game_count"`
		Games     []Game `json:"games"`
	}
	type APIResponse struct {
		Response Response `json:"response"`
	}
	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		log.Fatalf("Invalid response from Steam API: %v", err)
	}
	if len(apiResp.Response.Games) == 0 {
		fmt.Println("No games found.")
		return
	}
	games := apiResp.Response.Games
	sort.Slice(games, func(i, j int) bool {
		return games[i].Name < games[j].Name
	})

	var unplayedGames []string
	fmt.Printf("%s: No playtime recorded for these games:\n")
	for _, game := range games {
		if game.PlaytimeForever == 0 {
			fmt.Printf("%s: %.2f hours\n", game.Name, float64(game.PlaytimeForever)/60.0)

			unplayedGames = append(unplayedGames, game.Name)
		}
	}

	rand.Seed(time.Now().UnixNano())
	randomIndex := rand.Intn(len(unplayedGames))
	fmt.Printf("\n== Random Game Selection ==\n")
	fmt.Printf("Randomly selected game to play: %s\n", unplayedGames[randomIndex])
}
