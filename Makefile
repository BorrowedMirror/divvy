# Passthrough to Taskfile.yml: `make <task>` runs the task with Wails' embedded Task runner
# pinned in tools/go.mod, so nothing needs installing. Examples:
#
#   make check            make dev            make e2e:x11 RUN=TestTray
#
# NAME=value arguments are passed to the task. Every task is defined in Taskfile.yml: add or
# change tasks there, not here. Plain `make` lists them.
# The CLI is built without cgo: it only runs tasks, and on Linux a cgo build would need GTK4
# and WebKitGTK 6.0 headers (the app itself builds against GTK3). The tasks keep cgo.
TASK := $(shell CGO_ENABLED=0 go tool -modfile=tools/go.mod -n wails3) task

.DEFAULT_GOAL := default

# FORCE: run the task even when a file or folder has its name (build, web, e2e...).
%: FORCE
	@$(TASK) $@ $(MAKEOVERRIDES)

.PHONY: FORCE
FORCE:

# Not a task: keeps make from trying to rebuild this file through the rule above.
Makefile: ;

# `make release patch` (or minor, major): the bump is a goal of its own, which release passes on
# as BUMP; `make release BUMP=0.2.0-rc.1` names an exact version. Alone, a bump does nothing.
release: FORCE
	@bump="$(BUMP)"; for goal in $(MAKECMDGOALS); do case "$$goal" in patch|minor|major) bump="$$goal" ;; esac; done; \
	$(TASK) release BUMP="$$bump"

patch minor major: FORCE
	@case " $(MAKECMDGOALS) " in *" release "*) ;; *) echo "make $@: did you mean make release $@?" >&2; exit 1 ;; esac
