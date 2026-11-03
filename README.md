# cerrojo

An idle screen locker for Wayland compositors that speak
`ext-idle-notify-v1` (niri, sway, Hyprland, river). The idle timeout is
one of a few steps you cycle through with a key. Waybar shows the
current step, and once you stop touching the machine, a pie that fills
toward the lock and a countdown.

| state | bar |
|---|---|
| active | `󰌾 5m` |
| idle | `󰌾 󰪡 3:42` (the pie fills in eight slices) |
| never | `󰌿 ∞` |
| daemon down | `󰌾 ?` |

The glyphs are Nerd Fonts v3 (`nf-md-lock`, `nf-md-circle_slice_*`).

## Commands

    cerrojo run [--steps 5m,30m,1h,never] [--grace 10s] [--lock 'swaylock -f']
    cerrojo next | prev           cycle the steps; prints the new one
    cerrojo set 30m               jump to a step
    cerrojo status [--json]
    cerrojo waybar                JSON lines for a waybar custom module

`run` is the daemon. It counts idle time from the last input, so the
countdown starts `--grace` after you stop and already shows that much
gone. When the step runs out it runs `--lock` once; input clears that.
It also holds a logind delay inhibitor and locks before suspend
(`--lock-before-sleep=false` to leave that to something else). Idle
inhibitors such as a playing video stop the clock, as they would for
swayidle.

The step lives in `$XDG_RUNTIME_DIR/cerrojo.step`: it survives a daemon
restart and resets to the first step at the next login.

## Home Manager

```nix
inputs.cerrojo.url = "github:jack-work/cerrojo";

imports = [ inputs.cerrojo.homeManagerModules.default ];
services.cerrojo = {
  enable = true;
  steps = [ "5m" "30m" "1h" "never" ];
  lockCommand = "swaylock -f -c 000000";
};
```

This runs `cerrojo run` as a user unit bound to
`graphical-session.target`. The default `lockCommand` is nixpkgs'
swaylock. On a distribution whose swaylock comes from the system, point
it at `/usr/bin/swaylock`.

## Waybar

```jsonc
"custom/cerrojo": {
    "exec": "cerrojo waybar",
    "return-type": "json",
    "restart-interval": 2,
    "on-click": "cerrojo next",
    "on-click-right": "cerrojo prev"
}
```

CSS classes: `active`, `idle`, `soon` (the last 30 seconds), `never`,
`locked`, `down`.

## niri

```kdl
binds {
    Mod+I       { spawn "cerrojo" "next"; }
    Mod+Shift+I { spawn "cerrojo" "prev"; }
}
```
