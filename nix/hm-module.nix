self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.cerrojo;
  args = [
    "--steps"
    (lib.concatStringsSep "," cfg.steps)
    "--grace"
    cfg.grace
    "--lock"
    cfg.lockCommand
    "--lock-before-sleep=${lib.boolToString cfg.lockBeforeSleep}"
  ];
in
{
  options.services.cerrojo = {
    enable = lib.mkEnableOption "cerrojo, an idle screen locker with a countdown for waybar";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = lib.literalExpression "cerrojo.packages.\${system}.default";
      description = "The cerrojo package.";
    };

    steps = lib.mkOption {
      type = lib.types.nonEmptyListOf lib.types.str;
      default = [
        "5m"
        "30m"
        "1h"
        "never"
      ];
      description = ''
        Idle timeouts that `cerrojo next` and `cerrojo prev` cycle through,
        as Go durations or `never`. A fresh login starts on the first.
      '';
    };

    grace = lib.mkOption {
      type = lib.types.str;
      default = "10s";
      description = "How long the seat must be idle before the countdown shows.";
    };

    lockCommand = lib.mkOption {
      type = lib.types.str;
      default = "${lib.getExe pkgs.swaylock} -f";
      defaultText = lib.literalExpression ''"''${lib.getExe pkgs.swaylock} -f"'';
      description = ''
        Shell command that locks the screen and exits once the lock is up,
        as `swaylock -f` does.
      '';
    };

    lockBeforeSleep = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Hold a logind delay inhibitor and lock before the machine suspends.";
    };

    systemdTarget = lib.mkOption {
      type = lib.types.str;
      default = config.wayland.systemd.target;
      defaultText = lib.literalExpression "config.wayland.systemd.target";
      description = "The graphical session target the daemon belongs to.";
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];

    systemd.user.services.cerrojo = {
      Unit = {
        Description = "cerrojo idle screen locker";
        PartOf = [ cfg.systemdTarget ];
        After = [ cfg.systemdTarget ];
        ConditionEnvironment = "WAYLAND_DISPLAY";
      };
      Service = {
        ExecStart = "${lib.getExe cfg.package} run ${
          lib.replaceStrings [ "%" ] [ "%%" ] (lib.escapeShellArgs args)
        }";
        Restart = "always";
        RestartSec = 1;
        KillMode = "process";
      };
      Install.WantedBy = [ cfg.systemdTarget ];
    };
  };
}
