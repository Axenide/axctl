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
          default = pkgs.buildGoModule {
            pname = "axctl";
            version = "0.0.1";
            src = self;
            go = pkgs.go;
            subPackages = [ "." ];
            ldflags = [
              "-X"
              "main.Version=0.0.1"
            ];
            vendorHash = "sha256-4PUs37IRhUPtuXi4KU8wOUErIkVlcnaoj94zBDBsMdk=";
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
              pkgs.go
              pkgs.gopls
            ];
          };
        });
    };
}
