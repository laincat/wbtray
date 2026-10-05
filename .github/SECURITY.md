# Dependency scope

wbtray builds and publishes only `x86_64-pc-windows-msvc` binaries.

`Cargo.lock` also records Linux dependencies of `tray-icon` and `muda`, including
GLib 0.18.5. GHSA-wrw7-89jp-8q8g affects GLib before 0.20.0, but GLib is absent
from the Windows dependency graph and does not enter the published executable.
The upstream GTK 3 dependency chain currently requires GLib 0.18, so forcing
GLib 0.20 would not be a compatible upgrade.

The build workflow checks the locked Windows dependency graph and fails if GLib
appears. Reassess this advisory before adding a Linux build or changing the GUI
dependency chain. Do not treat the Linux dependency as patched.
