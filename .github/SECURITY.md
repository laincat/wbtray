# Dependency scope

wbtray builds and publishes only `x86_64-pc-windows-msvc` binaries.

On October 5, 2026, `tray-icon` was upgraded to 0.26.0 with default features
disabled. Its default Linux GTK backend is unnecessary for this Windows project.
This removes the GTK/GLib chain, including GLib 0.18.5 affected by
GHSA-wrw7-89jp-8q8g, from `Cargo.lock` and the dependency graph for all targets.

The unused `zip` dependency was also removed; the application does not use its
archive API.

The build workflow checks the locked dependency graph for all targets and fails
if GLib appears again. Reassess this advisory before enabling a Linux backend.
