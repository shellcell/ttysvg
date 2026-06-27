# AUR packaging

`PKGBUILD` builds `ttysvg-bin` from the prebuilt GitHub release tarballs
(x86_64 and aarch64).

## Publishing a new version

1. Bump `pkgver` (and reset `pkgrel=1`) in `PKGBUILD`.
2. Fill the real checksums: take the values from the release `checksums.txt`
   and replace the `SKIP` entries, or run `updpkgsums` in this directory.
3. Regenerate metadata: `makepkg --printsrcinfo > .SRCINFO`.
4. Test locally: `makepkg -si`.
5. Push to the AUR (separate repo):

   ```sh
   git clone ssh://aur@aur.archlinux.org/ttysvg-bin.git
   cp PKGBUILD .SRCINFO ttysvg-bin/
   cd ttysvg-bin && git commit -am "ttysvg 0.0.7" && git push
   ```

The tarball layout consumed here (`ttysvg`, `man/man1/ttysvg.1`,
`completions/*`, `LICENSE`) is produced by `make release` in the repo root.
