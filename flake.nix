{
  description = "A simple music player for the terminal";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
    in
    {
      packages.${system}.default = pkgs.buildGoModule {
        pname = "dirplay";
        version = "0.1.0";
        src = ./.;
        vendorHash = "sha256-uR9ZGMj3Z+6j2xq5ylAH6Pj29rWwToc+lRgOVqheFf0=";
        
        nativeBuildInputs = [ pkgs.pkg-config ];
        buildInputs = [ pkgs.alsa-lib ];
        
        postInstall = ''
          install -Dm644 ${./icon.svg} $out/share/icons/hicolor/scalable/apps/dirplay.svg
          install -Dm644 ${./dirplay.desktop} $out/share/applications/dirplay.desktop
        '';
      };
    };
}
