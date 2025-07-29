package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Game represents a game in the Steam library
// with its name and total playtime in minutes.
type Game struct {
	Name            string `json:"name"`
	PlaytimeForever int    `json:"playtime_forever"`
}

// APIResponse represents the structure of the response from the Steam API
// when fetching owned games.
type APIResponse struct {
	Response struct {
		GameCount int    `json:"game_count"`
		Games     []Game `json:"games"`
	} `json:"response"`
}

// getSteamIDFilePath returns the file path where the SteamID64 is stored.
// It uses the user's home directory and a fixed filename ".steamid".
// Arguments:
//   - None
// Returns the file path as a string and an error if the home directory cannot be determined.
func getSteamIDFilePath() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(usr.HomeDir, ".steamid"), nil
}


// saveSteamID64 saves the given SteamID64 to a file in the user's home directory.
// The file is created with permissions 0600 (read/write for the owner only).
// Arguments:
//   - steamID64: The SteamID64 to save.
// Returns an error if the file cannot be written.
func saveSteamID64(steamID64 string) error {	
	path, err := getSteamIDFilePath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(steamID64), 0600)
}

// loadSteamID64 reads the SteamID64 from the file in the user's home directory.
// It returns an error if the file does not exist or if the content is empty.
// Arguments:
//   - None
// Returns the SteamID64 as a string and an error if the file cannot be read or if the content is empty.
func loadSteamID64() (string, error) {
	path, err := getSteamIDFilePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", errors.New("stored steamid is empty")
	}
	return id, nil
}

// deleteSteamID64 deletes the file containing the SteamID64 from the user's home directory.
// It returns an error if the file cannot be removed.
// Arguments:
//   - None
// Returns an error if the file cannot be deleted.
func deleteSteamID64() error {
	path, err := getSteamIDFilePath()
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// getFreePort finds a free TCP port on the local machine.
// It listens on a random port and returns the port number as a string.
// Arguments:
//   - None
// Returns the free port as a string and an error if it cannot find a free port.
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

// openBrowser opens the given URI in the default web browser.
// It uses different commands based on the operating system:
// Arguments:
//   - uri: The URI to open in the browser.
// Returns an error if the command cannot be executed or if the platform is unsupported.
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

// promptYesNo prompts the user with a yes/no question and returns true for "yes" or "y".
// It reads input from the standard input and trims whitespace.
// Arguments:
//   - message: The question to prompt the user.
// Returns true if the user responds with "yes" or "y", false otherwise.
func promptYesNo(message string) bool {
	fmt.Print(message)
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

// main is the entry point of the program.
// It loads the Steam API key from the environment or .env file,
// checks for a saved SteamID64, prompts the user to refresh their login if desired,
// performs OpenID login if necessary, and lists the user's games using the Steam API.
func main() {
	_ = godotenv.Load()
	apiKey := os.Getenv("STEAM_API_KEY")
	if apiKey == "" {
		log.Fatal("STEAM_API_KEY not set in environment or .env file")
	}

	var steamID64 string
	steamID64, err := loadSteamID64()
	if err == nil {
		fmt.Println("✔️ Found saved SteamID64:", steamID64)
		if promptYesNo("Would you like to refresh your Steam login? (y/N): ") {
			if err := deleteSteamID64(); err != nil {
				log.Printf("Could not delete saved SteamID64: %v", err)
			}
			steamID64, err = performOpenIDLogin()
			if err != nil {
				log.Fatalf("Login failed: %v", err)
			}
			fmt.Println("✔️ Saving SteamID64 for next time:", steamID64)
			if err := saveSteamID64(steamID64); err != nil {
				fmt.Println("Warning: could not save SteamID64:", err)
			}
		} else {
			fmt.Println("Using saved SteamID64.")
		}
	} else {
		steamID64, err = performOpenIDLogin()
		if err != nil {
			log.Fatalf("Login failed: %v", err)
		}
		fmt.Println("✔️ Saving SteamID64 for next time:", steamID64)
		if err := saveSteamID64(steamID64); err != nil {
			fmt.Println("Warning: could not save SteamID64:", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		games, err := listGames(ctx, steamID64, apiKey)
		if err != nil {
			log.Fatalf("Error listing games: %v", err)
		}

		unplayed := unplayedGames(games, 120) // 2 hours threshold
		randomGame, err := getRandomUnplayedGame(unplayed)
		if err != nil {
			log.Printf("Couldn't pick a random unplayed game: %v", err)
		}
		leastPlayed, err := getLeastPlayedGame(games)
		if err != nil {
			log.Printf("Couldn't find least played game: %v", err)
		}

		fmt.Printf("== Welcome to WSPIN 1.0 ==\n")
		fmt.Printf("Total games: %d, Unplayed (<2h) games: %d\n", len(games), len(unplayed))

		if len(unplayed) > 0 {
			fmt.Printf("\n== Random Unplayed Game ==\n%s\n", randomGame.Name)
		}
		if leastPlayed.Name != "" {
			fmt.Printf("\n== Least Played Game ==\n%s (%d minutes)\n", leastPlayed.Name, leastPlayed.PlaytimeForever)
		}
	}

// performOpenIDLogin initiates the OpenID login process with Steam.
// Arguments:
//   - None
// Returns the SteamID64 as a string and an error if the login process fails.
func performOpenIDLogin() (string, error) {
	port, err := getFreePort()
	if err != nil {
		return "", fmt.Errorf("could not get free port: %v", err)
	}

	redirectURL := fmt.Sprintf("http://localhost:%s/callback", port)
	realmURL := fmt.Sprintf("http://localhost:%s", port)
	loginURL := fmt.Sprintf(
		"https://steamcommunity.com/openid/login"+
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

	fmt.Println("Opening Steam login in your browser...")
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
	return steamID64, nil
}

// listGames fetches the list of games owned by the user using the Steam API.
// It prints the total number of games, the number of unplayed games,
// and randomly selects one unplayed game to suggest to the user.
// Arguments:
//   - steamID64: The user's SteamID64.
//   - apiKey: The Steam API key to authenticate the request.
// Returns an error if the API request fails or if the response is invalid.

// listGames fetches and returns all games (sorted alphabetically). It does NOT print anything.
func listGames(ctx context.Context, steamID64, apiKey string) ([]Game, error) {
	apiURL := fmt.Sprintf(
		"https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?key=%s&steamid=%s&include_appinfo=1&include_played_free_games=1",
		apiKey, steamID64,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching games: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam API returned status %d", resp.StatusCode)
	}

	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("invalid response from Steam API: %w", err)
	}

	games := apiResp.Response.Games
	sort.Slice(games, func(i, j int) bool {
		return games[i].Name < games[j].Name
	})
	return games, nil
}

// unplayedGames returns all games with playtime < thresholdMinutes (e.g., < 120 = < 2h).
func unplayedGames(games []Game, thresholdMinutes int) []Game {
	out := make([]Game, 0, len(games))
	for _, g := range games {
		if g.PlaytimeForever < thresholdMinutes {
			out = append(out, g)
		}
	}
	return out
}

// getRandomUnplayedGame returns a random game from the given slice.
func getRandomUnplayedGame(unplayed []Game) (Game, error) {
	if len(unplayed) == 0 {
		return Game{}, errors.New("no unplayed games")
	}
	rand.Seed(time.Now().UnixNano())
	return unplayed[rand.Intn(len(unplayed))], nil
}

// getLeastPlayedGame returns the least played game overall.
// If there are no games, returns an error.
func getLeastPlayedGame(games []Game) (Game, error) {
	if len(games) == 0 {
		return Game{}, errors.New("no games")
	}
	min := games[0]
	for _, g := range games[1:] {
		if g.PlaytimeForever < min.PlaytimeForever {
			min = g
		}
	}
	return min, nil
}
// Note: The above code assumes that the .env file is properly set up with the STEAM_API_KEY.