{
  description = "Spage Go project with Temporal";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          nativeBuildInputs = [
            pkgs.go_1_24
            pkgs.temporal-cli
            pkgs.golangci-lint
            pkgs.ansible
            pkgs.python312Packages.distutils
            pkgs.python312Packages.setuptools
            pkgs.pre-commit
            # Add other Go tools or dependencies here if needed, e.g.:
            pkgs.gopls
            pkgs.delve
            pkgs.goreleaser
          ];

          # Disable hardening for cgo
          hardeningDisable = [ "all" ];

          # Set environment variables if necessary
          shellHook = ''
            export ANSIBLE_COLLECTIONS_PATH=./.ansible
            if [ ! -d ./.ansible ]; then
              ansible-galaxy collection install community.general --force
            fi
          '';
        };
      });
}
