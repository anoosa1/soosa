WARNING: This bot is not production ready. Only use in private servers.

# Soosa

Soosa is a Discord bot written in Go. It has interactive games, an economy system, and music playback with Subsonic/Navidrome servers.

## Features

### Games
  - Poker: Texas Hold'em with turn-based logic, betting, and an UI using Discord buttons and ephemeral messages.
  - Blackjack: Multi/Singleplayer Blackjack.
  - Wordle: Play Wordle directly natively in Discord with ephemeral progress tracking.
  - Integrated currency system for betting and rewards.
### Music
  - Audio streaming natively from any Subsonic API-compatible server (like Navidrome).
  - Queue management, player controls, and nowplaying display.
- Permissions: A modular permission node system for commands.
- Admin tools (kick, ban, mute, etc.)

## Installation

### Nix & NixOS

The repo contains a nix flake and a nixos module for easy installation and management.

#### Nix shell
You can run Soosa directly using `nix run`:
```bash
nix run github:anoosa1/soosa
```

#### Module
To run Soosa as a service on NixOS, you can import the provided module in your configuration.

1. Add Soosa to your flake inputs:
```nix
inputs.soosa.url = "github:anoosa1/soosa";
```

2. Import the module and configure it in your NixOS configuration (`configuration.nix` or similar):
```nix
imports = [
  inputs.soosa.nixosModules.soosa
];

services.soosa = {
  enable = true;
  environmentFile = "/run/secrets/soosa"; 
};
```

### Prerequisites
- Go 1.20 or newer
- `ffmpeg` installed and accessible in your system's PATH
- A Discord Bot Token
- (Optional) A Subsonic/Navidrome server for music streaming

### Setup

1. **Clone the repository**
   ```bash
   git clone https://github.com/yourusername/soosa.git
   cd soosa
   ```

2. **Configure Environment Variables**
   Rename `.env.example` to `.env` and fill in your details:
   ```env
   DISCORD_TOKEN=your_token_here
   GUILD_ID=your_guild_id_here
   LOG_CHANNEL_ID=your_log_channel_id_here
   SUBSONIC_URL=https://your-subsonic-server.example.com
   SUBSONIC_USER=your_user
   SUBSONIC_PASSWORD=your_password
   ```

3. **Build the Bot**
   ```bash
   # Download dependencies and build
   go build -v -o soosa ./cmd/bot/main.go
   ```

4. **Run**
   ```bash
   ./soosa
   ```

## Usage

Most interactions happen through Discord slash commands and interactive buttons. Make sure the bot is invited to your server with `applications.commands` and standard message reading/writing permissions.
