{ pkgs ? import <nixpkgs> { } }:

pkgs.mkShell {
  buildInputs = [
    pkgs.go
    pkgs.gcc
    pkgs.pkg-config
    pkgs.alsa-lib
  ];

  shellHook = ''
    export CGO_ENABLED=1
    export CGO_CFLAGS="-I${pkgs.alsa-lib.dev}/include"
    export CGO_LDFLAGS="-L${pkgs.alsa-lib}/lib"
  '';
}
