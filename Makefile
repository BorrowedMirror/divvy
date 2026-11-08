# Passthrough to Taskfile.yml: `make <task>` runs the task with Wails' embedded Task runner
# pinned in tools/go.mod, so nothing needs installing. Examples:
#
#   make check            make dev            make e2e:x11 RUN=TestTray
#
# NAME=value arguments are passed to the task. Every task is defined in Taskfile.yml: add or
# change tasks there, not here. Plain `make` lists them.
TASK := go tool -modfile=tools/go.mod wails3 task

.DEFAULT_GOAL := default

# FORCE: run the task even when a file or folder has its name (build, web, e2e...).
%: FORCE
	@$(TASK) $@ $(MAKEOVERRIDES)

.PHONY: FORCE
FORCE:

# Not a task: keeps make from trying to rebuild this file through the rule above.
Makefile: ;
