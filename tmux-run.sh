#! /usr/bin/env bash

tmux new-session -d -s irc2slack -n irc2slack -d "cd ~/src/github.com/fredsmith/irctoslack; go run irc2slack.go"
