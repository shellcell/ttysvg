{
  description = "Record terminal sessions as animated SVG";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = "0.0.7";
    in
    {
      packages = forAllSystems (pkgs: rec {
        ttysvg = pkgs.buildGoModule {
          pname = "ttysvg";
          inherit version;
          src = ./.;

          vendorHash = "sha256-eL0tt6+aK1D5KhJDJsBIgy/BcDWyKtE4TFZf6mmEjPo=";

          subPackages = [ "cmd/ttysvg" ];
          ldflags = [ "-s" "-w" "-X main.version=${version}" ];

          nativeBuildInputs = [ pkgs.installShellFiles ];
          postInstall = ''
            installManPage docs/ttysvg.1
            installShellCompletion --bash --name ttysvg.bash completions/ttysvg.bash
            installShellCompletion --fish --name ttysvg.fish completions/ttysvg.fish
            installShellCompletion --zsh --name _ttysvg completions/_ttysvg
          '';

          meta = with pkgs.lib; {
            description = "Record terminal sessions as animated SVG";
            homepage = "https://github.com/rabarbra/ttysvg";
            license = licenses.mit;
            mainProgram = "ttysvg";
            platforms = platforms.unix;
          };
        };
        default = ttysvg;
      });

      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          program = "${self.packages.${pkgs.system}.ttysvg}/bin/ttysvg";
        };
      });
    };
}
