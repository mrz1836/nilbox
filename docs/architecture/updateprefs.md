# internal/updateprefs

`internal/updateprefs/` declares the update setting, `update.mode` (`Mode`: `download`, the
default, "Download it and tell me"; `notify`, "Just tell me"; `off`, "Don't check"), which
settings alone writes (`SetUpdateMode`, emitting the Go-only `event.UpdatesChanged`) and
backups carry (`updateMode`), and the update package reads. It is a leaf that imports nothing
of the app's.
