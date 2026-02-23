{ self, ... }: {
  flake.nixosModules.soosa = { config, lib, pkgs, ... }:
  let
    cfg = config.services.soosa;
  in {
    options.services.soosa = {
      enable = lib.mkEnableOption "soosa Discord bot";

      package = lib.mkOption {
        type = lib.types.package;
        default = self.packages.${pkgs.system}.soosa;
        description = "The soosa-bot package to use.";
      };

      environmentFile = lib.mkOption {
        type = lib.types.path;
        description = "Path to an environment file containing DISCORD_TOKEN and other secrets. This must be accessible by the systemd dynamic user.";
        example = "/run/secrets/soosa";
      };

      databasePath = lib.mkOption {
        type = lib.types.str;
        default = "/var/lib/soosa/permissions.db";
        description = "Path to the SQLite database. Configures the DATABASE environment variable. Will be persisted across reboots if under /var/lib/soosa-bot.";
      };

      environment = lib.mkOption {
        type = lib.types.attrsOf lib.types.str;
        default = {};
        description = "Extra environment variables to pass to the bot.";
      };
    };

    config = lib.mkIf cfg.enable {
      systemd.services.soosa = {
        description = "soosa Discord bot";
        after = [ "network-online.target" ];
        wantedBy = [ "multi-user.target" ];

        environment = {
          DATABASE = cfg.databasePath;
          WORDLE_ANSWERS_PATH = "${cfg.package}/share/soosa/wordlist_answers.txt";
          WORDLE_ALLOWED_PATH = "${cfg.package}/share/soosa/wordlist_allowed.txt";
        } // cfg.extraEnvironment;

        serviceConfig = {
          ExecStart = "${cfg.package}/bin/bot";
          Restart = "always";
          RestartSec = "10s";
          EnvironmentFile = cfg.environmentFile;
          StateDirectory = "soosa";
          WorkingDirectory = "/var/lib/soosa";
          DynamicUser = true;
        };
      };
    };
  };

  perSystem = { pkgs, lib, ... }: {
    packages.soosa = pkgs.buildGoModule {
      pname = "soosa";
      version = "0.1.0";

      src = ./.;

      vendorHash = "sha256-tl3sKqzfCeB2I0m8iOLHYeWhd6HxJnqcRj2q2fM0hjM=";

      subPackages = [ "cmd/bot" ];

      postInstall = ''
        mkdir -p $out/share/soosa
        cp wordlist_*.txt $out/share/soosa/ || true
      '';

      meta = with lib; {
        description = "Soosa Discord Bot";
        license = licenses.gpl3;
        platforms = platforms.linux;
        maintainers = with maintainers; [ anoosa ];
        mainProgram = "bot";
      };
    };
  };
}
