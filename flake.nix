{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    treefmt-nix.url = "github:numtide/treefmt-nix";
    treefmt-nix.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs =
    inputs@{
      flake-parts,
      treefmt-nix,
      ...
    }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      imports = [ treefmt-nix.flakeModule ];

      systems = [
        "aarch64-darwin"
        "aarch64-linux"
        "x86_64-darwin"
        "x86_64-linux"
      ];

      perSystem =
        {
          config,
          pkgs,
          ...
        }:
        {
          treefmt = {
            projectRootFile = "flake.nix";
            programs.gofmt.enable = true;
            programs.nixfmt.enable = true;
          };

          devShells.default = pkgs.mkShell {
            packages = [
              pkgs.git
              pkgs.go_1_26
              pkgs.gh
              pkgs.go-tools
              pkgs.skills
            ];
          };
        };
    };
}
