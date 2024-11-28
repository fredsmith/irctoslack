# irctoslack

A bidirectional bridge between IRC and Slack channels, allowing seamless communication between IRC and Slack users.

## Features

- Bidirectional message relay between IRC and Slack
- Proper handling of IRC actions (/me) and join/part messages
- User display name support for Slack messages
- Translation of Slack @mentions to readable usernames
- Bot message filtering to prevent loops
- Efficient user information caching
- Automatic reconnection for IRC
- Thread-safe message handling

## Prerequisites

- Go 1.13 or higher
- A Slack workspace where you can create apps
- A server with public internet access (e.g., DigitalOcean droplet)

## Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/fredsmith/irctoslack.git
   cd irctoslack
   ```

2. Build the application:
   ```bash
   go build
   ```

## Slack Configuration

1. Create a new Slack App:
   - Go to https://api.slack.com/apps
   - Click "Create New App"
   - Choose "From scratch"
   - Name your app and select your workspace

2. Configure OAuth & Permissions:
   - Navigate to "OAuth & Permissions" in your app settings
   - Under "Scopes", add the following Bot Token Scopes:
     * `users:read` - For looking up user information
     * `users:read.email` - For complete user profile access
   - Install the app to your workspace
   - Copy the "Bot User OAuth Token" (starts with `xoxb-`)

3. Configure Event Subscriptions:
   - Navigate to "Event Subscriptions"
   - Toggle "Enable Events" to On
   - Set the Request URL to `http://your-server:3000/webhook`
   - Under "Subscribe to bot events", add:
     * `message.channels` - For public channel messages
   - Save Changes

## Application Configuration

1. Create or modify `config.yaml`:
   ```yaml
   irc:
     server: "irc.oftc.net:6667"  # Your IRC server
     channel: "#yourchannel"       # Your IRC channel
     nickname: "slackbridge"       # Bot's nickname on IRC

   slack:
     webhook_url: "https://hooks.slack.com/services/..."  # Your Slack webhook URL
     listen_address: ":3000"       # Local address to listen for Slack events
     api_token: "xoxb-..."        # Your Bot User OAuth Token
     ignore_bots: true            # Ignore bot messages to prevent loops
     ignore_users: []             # List of Slack user IDs to ignore
   ```

2. Set proper permissions:
   ```bash
   chmod 600 config.yaml  # Protect the config file containing sensitive tokens
   ```

## Running the Application

1. Start the application:
   ```bash
   ./irctoslack
   ```

2. For production use, consider using a process manager like systemd. Create `/etc/systemd/system/irctoslack.service`:
   ```ini
   [Unit]
   Description=IRC to Slack bridge
   After=network.target

   [Service]
   Type=simple
   User=irctoslack
   WorkingDirectory=/path/to/irctoslack
   ExecStart=/path/to/irctoslack/irctoslack
   Restart=always
   RestartSec=5

   [Install]
   WantedBy=multi-user.target
   ```

3. Enable and start the service:
   ```bash
   sudo systemctl enable irctoslack
   sudo systemctl start irctoslack
   ```

## Message Translation

The bridge handles several types of message translations:

1. User Display Names:
   - Slack user IDs are automatically translated to display names
   - Display names are cached for 1 hour to minimize API calls
   - Falls back to real name if display name is empty
   - Falls back to user ID if both are empty

2. @mentions:
   - Slack @mentions (e.g., <@U1234ABCD>) are automatically translated to readable usernames
   - Uses the same caching mechanism as display names
   - Appears as @username in IRC

3. Special Messages:
   - IRC /me actions are formatted with italics in Slack
   - Join/Part messages are formatted with asterisks in Slack
   - Bot messages can be filtered to prevent loops

## Firewall Configuration

Ensure your server's firewall allows:
- Outbound connections to your IRC server (typically port 6667)
- Inbound connections to your webhook listener (port 3000 by default)

For UFW:
```bash
sudo ufw allow 3000/tcp
```

## Monitoring

Monitor the application logs:
- If running directly: Output goes to stdout/stderr
- If using systemd: `sudo journalctl -u irctoslack -f`

## Troubleshooting

1. Webhook URL not working:
   - Verify the server is accessible from the internet
   - Check firewall rules
   - Ensure the correct port is configured

2. Messages not appearing:
   - Check IRC connection status in logs
   - Verify Slack token permissions
   - Ensure bot is invited to the Slack channel

3. User names showing as IDs:
   - Verify the API token has proper user read permissions
   - Check logs for API rate limiting messages
   - Ensure the cache isn't being cleared unexpectedly

4. @mentions not translating:
   - Check logs for any API errors
   - Verify the mentioned users exist in the workspace
   - Ensure the bot has access to user information

## Security Considerations

- Keep your `config.yaml` secure as it contains sensitive tokens
- Use HTTPS if exposing the webhook endpoint to the internet
- Consider running behind a reverse proxy for additional security
- Regularly rotate Slack tokens
- Monitor logs for unauthorized access attempts

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
