{
  description = "axctl IPC daemon and CLI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.buildGoLatestModule {
            pname = "axctl";
            version = "0.0.21";
            src = self;
            subPackages = [ "." ];
            ldflags = [
              "-X"
              "main.Version=0.0.21"
            ];
            vendorHash = "sha256-aF1l/9CL2hV2ZLn7OGKoWLC3r/+KU/41/4DXTlEgVSc=";
          };
        });

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/axctl";
        };
      });

      devShells = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.go_latest
              pkgs.gopls
            ];
          };
        });
    };
}
