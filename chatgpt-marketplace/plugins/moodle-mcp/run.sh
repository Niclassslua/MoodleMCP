#!/bin/sh
export MCD_ENV_FILE="$HOME/.config/moodle-mcp/.env"
exec "$HOME/.local/bin/moodle-mcp" mcp serve
