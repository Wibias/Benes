# benes-run

Bound a long job on a Linux host (or Git Bash with GNU `timeout`) so it cannot
run forever, cannot stack a second copy of the same name, and cannot look live
after it has already exited.

Install: copy `scripts/benes-run` to `~/bin/benes-run` and `chmod +x` it.

## CLI

```
benes-run <job> <workdir> <limit> <command...>
benes-run status [job]
benes-run log <job> [lines]
benes-run kill <job>
```

`tail` is an alias of `log`. `stop` is an alias of `kill`. `start` is accepted
in front of a launch line.

- `<workdir>` must already exist. The child starts there. The caller cwd is ignored.
- A second live `<job>` is refused. It is not queued.
- `<limit>` is a GNU `timeout(1)` duration. SIGTERM first, SIGKILL 60 seconds later.
- The job is a new session so `kill` and the timer can tear down children.
- `$BENES_RUN_DIR` (default `~/.benes-run`) holds one directory per job:
  `held` (regular file with the owning runner pid), `pid`, `log`, `state.json`, `gate` (short-lived arbitration lock), plus a generated `launch`.
- The claim is the runner process, not the child. The runner writes `owner.<pid>` then `ln` that prepared file to `held`, so the published claim already names its owner. A missing child pid does not make the name free.
- Inspecting occupancy, clearing stale `held` / `pid` / `stop-requested`, and publishing a new owner all run under a short-lived exclusive lock on `gate` (`flock` when present; otherwise perl `Fcntl` `LOCK_EX`, which Git Bash provides). The lock is released before the child starts. It is not held for the job lifetime. A second start is refused while the owner pid or the recorded child pid is alive. A dead owner is not enough to reclaim if the child is still live. Historical `log` and `state.json` stay.
- `kill` only signals the job. The owning runner writes the terminal state and drops `held` after `wait` returns, so a replacement cannot start while the old runner can still write `pid` or `state.json`.
- `state.json` is one line: `started`, `ok`, `fail`, `timeout`, or `stopped`.
- The child PATH prepends `~/.local/bin` and `~/bin` (non-login ssh is otherwise bare).

## Example

```bash
ssh host 'export PATH=$HOME/bin:$PATH
  nohup benes-run suite ~/benes-boundary/repo 40m go test ./... > /dev/null 2>&1 &'

ssh host 'export PATH=$HOME/bin:$PATH; benes-run status'
ssh host 'export PATH=$HOME/bin:$PATH; benes-run log suite 40'
ssh host 'export PATH=$HOME/bin:$PATH; benes-run kill suite'
```

Live status includes seconds since the last log write. A `timeout` line is a hang
to investigate, not a cue to raise the limit.
