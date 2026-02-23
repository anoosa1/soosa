{
  description = "Soosa Discord Bot";

  inputs = {
    nixpkgs = {
      url = "github:NixOS/nixpkgs/nixos-unstable";
    };

    # flake-parts
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
    };
  };

  outputs = inputs: inputs.flake-parts.lib.mkFlake { inherit inputs; } {
    systems = [ "x86_64-linux" ]; 
    imports = [ ./soosa.nix ];
  };
}