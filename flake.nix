{
  description = "cerrojo: idle screen locking you can cycle, with a countdown for waybar";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.buildGoModule {
          pname = "cerrojo";
          version = "0.1.0";
          src = self;
          vendorHash = "sha256-y+3dL8WEiN4PDHLX9/iNijS20wUheiJ+mZrM7af69G0=";
          env.CGO_ENABLED = 0;
          ldflags = [
            "-s"
            "-w"
          ];
          meta = {
            description = "Idle screen locker for Wayland with steps you can cycle and a waybar countdown";
            mainProgram = "cerrojo";
            platforms = pkgs.lib.platforms.linux;
          };
        };
      });

      overlays.default = final: _: {
        cerrojo = self.packages.${final.stdenv.hostPlatform.system}.default;
      };

      homeManagerModules.default = import ./nix/hm-module.nix self;

      checks = forAllSystems (pkgs: {
        default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
          ];
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);
    };
}
