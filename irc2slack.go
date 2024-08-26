
package main

import (
    "bufio"
    "fmt"
    "gopkg.in/yaml.v2"
    "io/ioutil"
    "log"
    "net"
    "net/http"
    "strings"
    "time"
)

// Config structure to hold the yaml configuration
type Config struct {
    IRC struct {
        Server   string `yaml:"server"`
        Channel  string `yaml:"channel"`
        Nickname string `yaml:"nickname"`
    } `yaml:"irc"`
    Slack struct {
        WebhookURL string `yaml:"webhook_url"`
    } `yaml:"slack"`
}

func main() {
    config := loadConfig("config.yaml")
    for {
        err := connectAndListen(config)
        if err != nil {
            log.Printf("Error: %v", err)
            log.Println("Reconnecting in 5 seconds...")
            time.Sleep(5 * time.Second)
        }
    }
}

func connectAndListen(config *Config) error {
    conn, err := net.Dial("tcp", config.IRC.Server)
    if err != nil {
        return fmt.Errorf("failed to connect to IRC server: %v", err)
    }
    defer conn.Close()

    // Sending IRC commands
    fmt.Fprintf(conn, "NICK %s
", config.IRC.Nickname)
    fmt.Fprintf(conn, "USER %s 8 * :%s
", config.IRC.Nickname, config.IRC.Nickname)
    fmt.Fprintf(conn, "JOIN %s
", config.IRC.Channel)

    // Reading messages
    reader := bufio.NewReader(conn)
    for {
        message, err := reader.ReadString('
')
        if err != nil {
            return fmt.Errorf("error reading message: %v", err)
        }
        handleMessage(message, conn, config.Slack.WebhookURL)
    }
}

func handleMessage(message string, conn net.Conn, slackWebhookURL string) {
    // Print message to console (for debugging)
    fmt.Print(message)

    // Respond to PING messages to avoid being disconnected
    if strings.HasPrefix(message, "PING") {
        response := strings.Replace(message, "PING", "PONG", 1)
        fmt.Fprintf(conn, response)
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
    if strings.Contains(message, "PRIVMSG") && strings.Contains(message, "ACTION") {
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
    messageParts := strings.SplitN(message, ":", 3)
    if len(messageParts) > 2 {
        return messageParts[2]
    }
    return ""
}

// Extract the ACTION message (/me command)
func extractActionMessage(message string) string {
    start := strings.Index(message, "ACTION") + len("ACTION ")
    end := strings.Index(message[start:], "")
    if end == -1 {
        return message[start:]
    }
    return message[start : start+end]
}

func postToSlack(message, slackWebhookURL string) {
    // Escape special characters in the message
    escapedMessage := strings.ReplaceAll(message, `"`, `"`)

    // Prepare the payload for the Slack webhook
    payload := fmt.Sprintf(`{"text": "%s"}`, escapedMessage)
    fmt.Println("Payload:", payload) // Print the payload for debugging

    resp, err := http.Post(slackWebhookURL, "application/json", strings.NewReader(payload))
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
