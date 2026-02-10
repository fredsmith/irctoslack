package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
)

// Config structure to hold the yaml configuration
type Config struct {
	IRC struct {
		Server   string `yaml:"server"`
		Channel  string `yaml:"channel"`
		Nickname string `yaml:"nickname"`
	} `yaml:"irc"`
	Slack struct {
		WebhookURL    string   `yaml:"webhook_url"`
		ListenAddress string   `yaml:"listen_address"`
		APIToken      string   `yaml:"api_token"`
		IgnoreBots    bool     `yaml:"ignore_bots"`
		IgnoreUsers   []string `yaml:"ignore_users"`
	} `yaml:"slack"`
}

// IRCConnection holds the connection and related data
type IRCConnection struct {
	conn   net.Conn
	mutex  sync.Mutex
	config *Config
}

// SlackEvent represents the structure of incoming Slack events
type SlackEvent struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Event     struct {
		Type    string `json:"type"`
		User    string `json:"user"`
		Text    string `json:"text"`
		Channel string `json:"channel"`
		BotID   string `json:"bot_id,omitempty"`
		Subtype string `json:"subtype,omitempty"`
	} `json:"event"`
}

// SlackUserInfo represents user information from Slack API
type SlackUserInfo struct {
	Ok   bool `json:"ok"`
	User struct {
		Profile struct {
			DisplayName string `json:"display_name"`
			RealName    string `json:"real_name"`
		} `json:"profile"`
	} `json:"user"`
}

// UserCache holds user display names with expiration
type UserCache struct {
	displayName string
	expiration  time.Time
}

var (
	// Cache user info for 1 hour
	userCache     = make(map[string]UserCache)
	userCacheMux  sync.RWMutex
	cacheDuration = 1 * time.Hour
	// Regex for finding user mentions in Slack messages
	mentionRegex = regexp.MustCompile(`<@(U[A-Z0-9]+)>`)
)

func translateMentions(text string, config *Config) string {
	return mentionRegex.ReplaceAllStringFunc(text, func(mention string) string {
		matches := mentionRegex.FindStringSubmatch(mention)
		if len(matches) < 2 {
			return mention
		}
		userID := matches[1]
		displayName := getUserDisplayName(userID, config)
		return "@" + displayName
	})
}

func getUserDisplayName(userID string, config *Config) string {
	// Check cache first
	userCacheMux.RLock()
	if cache, exists := userCache[userID]; exists && time.Now().Before(cache.expiration) {
		userCacheMux.RUnlock()
		return cache.displayName
	}
	userCacheMux.RUnlock()

	// Fetch from Slack API
	url := fmt.Sprintf("https://slack.com/api/users.info?user=%s", userID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return userID
	}

	req.Header.Add("Authorization", "Bearer "+config.Slack.APIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Error fetching user info: %v", err)
		return userID
	}
	defer resp.Body.Close()

	var userInfo SlackUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		log.Printf("Error decoding user info: %v", err)
		return userID
	}

	if !userInfo.Ok {
		log.Printf("Error from Slack API for user %s", userID)
		return userID
	}

	// Use display name if set, otherwise use real name
	displayName := userInfo.User.Profile.DisplayName
	if displayName == "" {
		displayName = userInfo.User.Profile.RealName
	}
	if displayName == "" {
		displayName = userID
	}

	// Update cache
	userCacheMux.Lock()
	userCache[userID] = UserCache{
		displayName: displayName,
		expiration:  time.Now().Add(cacheDuration),
	}
	userCacheMux.Unlock()

	return displayName
}

func main() {
	generateConfig := flag.Bool("generate-config", false, "Generate a sample config.yaml with instructions")
	daemonize := flag.Bool("d", false, "Run in the background, logging to irc2slack.log")
	flag.Parse()

	if *generateConfig {
		printSampleConfig()
		return
	}

	if _, err := os.Stat("config.yaml"); os.IsNotExist(err) {
		printUsage()
		os.Exit(1)
	}

	if *daemonize {
		daemonizeProcess()
		return
	}

	config := loadConfig("config.yaml")

	// Create a channel to signal connection status
	connectionReady := make(chan *IRCConnection)

	// Start IRC connection management
	go manageIRCConnection(config, connectionReady)

	// Wait for initial connection
	ircConn := <-connectionReady

	// Start webhook listener
	log.Printf("Starting Slack webhook listener on %s", config.Slack.ListenAddress)
	http.HandleFunc("/webhook", createWebhookHandler(ircConn))
	if err := http.ListenAndServe(config.Slack.ListenAddress, nil); err != nil {
		log.Fatalf("Failed to start webhook listener: %v", err)
	}
}

func printUsage() {
	fmt.Println(`irctoslack - Bidirectional IRC to Slack bridge

Usage: irctoslack [options]

Options:
  --generate-config  Generate a sample config.yaml with instructions
  -d                 Run in the background, logging to irc2slack.log

irctoslack requires a config.yaml file in the current directory.
Run with --generate-config to create one.`)
}

func printSampleConfig() {
	fmt.Println(`# irctoslack configuration

# IRC settings
irc:
  # IRC server address and port
  server: "irc.oftc.net:6667"
  # Channel to join (include the #)
  channel: "#yourchannel"
  # Nickname for the bot on IRC
  nickname: "slackbridge"

# Slack settings
slack:
  # Incoming webhook URL for posting messages to Slack
  # Create one at https://api.slack.com/apps -> Incoming Webhooks
  webhook_url: "https://hooks.slack.com/services/T.../B.../..."
  # Address to listen on for Slack event webhooks
  listen_address: ":3000"
  # Bot User OAuth Token (starts with xoxb-)
  # Required scopes: users:read, users:read.email
  api_token: "xoxb-..."
  # Ignore messages from bots (recommended to prevent loops)
  ignore_bots: true
  # List of Slack user IDs to ignore
  ignore_users: []`)
}

func daemonizeProcess() {
	executable, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}

	// Rebuild args without -d
	var args []string
	for _, arg := range os.Args[1:] {
		if arg != "-d" {
			args = append(args, arg)
		}
	}

	logFile, err := os.OpenFile("irc2slack.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}

	cmd := exec.Command(executable, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start background process: %v", err)
	}

	fmt.Printf("irctoslack started in background (PID %d), logging to irc2slack.log\n", cmd.Process.Pid)
}

func shouldProcessMessage(event *SlackEvent, config *Config) bool {
	// Ignore messages with subtypes (like bot_message, message_changed, etc.)
	if event.Event.Subtype != "" {
		return false
	}

	// Ignore bot messages if configured
	if config.Slack.IgnoreBots && event.Event.BotID != "" {
		return false
	}

	// Check if user is in ignore list
	for _, ignoredUser := range config.Slack.IgnoreUsers {
		if event.Event.User == ignoredUser {
			return false
		}
	}

	return true
}

func createWebhookHandler(ircConn *IRCConnection) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var event SlackEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			log.Printf("Error decoding webhook payload: %v", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Handle URL verification challenge
		if event.Type == "url_verification" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(event.Challenge))
			return
		}

		// Handle message events
		if event.Type == "event_callback" && event.Event.Type == "message" {
			// Check if we should process this message
			if !shouldProcessMessage(&event, ircConn.config) {
				w.WriteHeader(http.StatusOK)
				return
			}

			// Get user's display name
			displayName := getUserDisplayName(event.Event.User, ircConn.config)

			// Translate any @mentions in the message
			translatedText := translateMentions(event.Event.Text, ircConn.config)

			// Send message to IRC using the shared connection
			ircMessage := fmt.Sprintf("PRIVMSG %s :<%s> %s\r\n",
				ircConn.config.IRC.Channel,
				displayName,
				translatedText)

			// Use mutex to ensure thread-safe writes to the connection
			ircConn.mutex.Lock()
			_, err := fmt.Fprintf(ircConn.conn, ircMessage)
			ircConn.mutex.Unlock()

			if err != nil {
				log.Printf("Error sending message to IRC: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
		}

		// Acknowledge receipt of the event
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}

func manageIRCConnection(config *Config, ready chan<- *IRCConnection) {
	var ircConn *IRCConnection
	firstConnection := true

	for {
		conn, err := net.Dial("tcp", config.IRC.Server)
		if err != nil {
			log.Printf("Failed to connect to IRC server: %v", err)
			if firstConnection {
				log.Fatalf("Failed to establish initial IRC connection")
			}
			continue
		}

		ircConn = &IRCConnection{
			conn:   conn,
			config: config,
		}

		// Send IRC authentication
		fmt.Fprintf(conn, "NICK %s\r\n", config.IRC.Nickname)
		fmt.Fprintf(conn, "USER %s 8 * :%s\r\n", config.IRC.Nickname, config.IRC.Nickname)
		fmt.Fprintf(conn, "JOIN %s\r\n", config.IRC.Channel)

		if firstConnection {
			ready <- ircConn
			firstConnection = false
		}

		// Handle incoming IRC messages
		reader := bufio.NewReader(conn)
		for {
			message, err := reader.ReadString('\n')
			if err != nil {
				log.Printf("Error reading from IRC: %v", err)
				break
			}
			handleMessage(message, ircConn, config.Slack.WebhookURL)
		}

		// If we get here, the connection was lost
		log.Println("IRC connection lost, reconnecting...")
	}
}

func handleMessage(message string, ircConn *IRCConnection, slackWebhookURL string) {
	// Print message to console (for debugging)
	fmt.Print(message)

	// Respond to PING messages to avoid being disconnected
	if strings.HasPrefix(message, "PING") {
		response := strings.Replace(message, "PING", "PONG", 1)
		ircConn.mutex.Lock()
		fmt.Fprintf(ircConn.conn, response)
		ircConn.mutex.Unlock()
		return
	}

	// Detect JOIN event
	if strings.Contains(message, "JOIN") {
		nickname := extractNickname(message)
		formattedMessage := fmt.Sprintf("*%s has joined the channel*", nickname)
		postToSlack(formattedMessage, slackWebhookURL)
		return
	}

	// Detect PART event
	if strings.Contains(message, "PART") {
		nickname := extractNickname(message)
		formattedMessage := fmt.Sprintf("*%s has left the channel*", nickname)
		postToSlack(formattedMessage, slackWebhookURL)
		return
	}

	// Detect ACTION (/me) event
	if strings.Contains(message, "PRIVMSG") && strings.Contains(message, "ACTION") {
		nickname := extractNickname(message)
		actionMessage := extractActionMessage(message)
		formattedMessage := fmt.Sprintf("_%s %s_", nickname, actionMessage)
		postToSlack(formattedMessage, slackWebhookURL)
		return
	}

	// Handle regular PRIVMSG (chat messages)
	if strings.Contains(message, "PRIVMSG") {
		nickname := extractNickname(message)
		ircMessage := extractIRCMessage(message)
		formattedMessage := fmt.Sprintf("<%s> %s", nickname, ircMessage)
		postToSlack(formattedMessage, slackWebhookURL)
	}
}

// Extract the nickname from an IRC message
func extractNickname(message string) string {
	prefixEnd := strings.Index(message, "!")
	if prefixEnd == -1 {
		return ""
	}
	return message[1:prefixEnd]
}

// Extract the regular IRC message
func extractIRCMessage(message string) string {
	// Find the PRIVMSG command, then extract the trailing message after " :"
	// This avoids splitting on colons in IPv6 addresses
	idx := strings.Index(message, " PRIVMSG ")
	if idx == -1 {
		return ""
	}
	rest := message[idx+len(" PRIVMSG "):]
	colonIdx := strings.Index(rest, " :")
	if colonIdx == -1 {
		return ""
	}
	return strings.TrimRight(rest[colonIdx+2:], "\r\n")
}

// Extract the ACTION message (/me command)
func extractActionMessage(message string) string {
	start := strings.Index(message, "ACTION") + len("ACTION ")
	end := strings.Index(message[start:], "\x01")
	if end == -1 {
		return strings.TrimRight(message[start:], "\r\n")
	}
	return message[start : start+end]
}

func postToSlack(message, slackWebhookURL string) {
	// Use json.Marshal for proper encoding of emoji, newlines, etc.
	payload := map[string]string{"text": message}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error encoding message to JSON: %v", err)
		return
	}
	fmt.Println("Payload:", string(jsonData)) // Print the payload for debugging

	resp, err := http.Post(slackWebhookURL, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		log.Printf("Error sending message to Slack: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Received non-OK response from Slack: %s", resp.Status)
	}
}

func loadConfig(filename string) *Config {
	config := &Config{}
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}
	err = yaml.Unmarshal(data, config)
	if err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}
	return config
}
