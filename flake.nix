{
  description = "A flake for nixpkgs-stats frontend";

  inputs = {
    # Nixpkgs / NixOS version to use.
    # nixpkgs.url = "nixpkgs/nixos-24.05";
    nixpkgs.url = "nixpkgs/master";
  };

  outputs =
    {
      self,
      nixpkgs,
    }:
    let
      # System types to support.
      supportedSystems = [ "x86_64-linux" ];

      # Helper function to generate an attrset '{ x86_64-linux = f "x86_64-linux"; ... }'.
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;

      # Nixpkgs instantiated for supported system types.
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });

    in
    {
      # Add dependencies that are only needed for development
      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          default = pkgs.mkShell {
            packages = [
              # it's possible to add this package as an override to nixpkgs; here it's just
              # added directly...
              pkgs.go
              pkgs.lazygit
            ];
          };
        }
      );
    };
}
